package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"esm/libs/go-common/contracts"
)

const (
	streamName   = "ESM"
	streamMaxAge = 7 * 24 * time.Hour
	maxDeliver   = 5
)

// NATS is a JetStream-backed Bus. All subjects live in one stream ("ESM", subjects "esm.>").
type NATS struct {
	nc *nats.Conn
	js jetstream.JetStream
}

// NewNATS connects (retrying until ctx is done) and makes sure the stream exists.
func NewNATS(ctx context.Context, url string) (*NATS, error) {
	nc, err := nats.Connect(url,
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	// The connection may still be establishing; retry stream creation until it works.
	for {
		_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:       streamName,
			Subjects:   []string{"esm.>"},
			Storage:    jetstream.FileStorage,
			MaxAge:     streamMaxAge,
			Duplicates: 10 * time.Minute,
		})
		if err == nil {
			break
		}
		slog.Warn("waiting for NATS JetStream", "url", url, "error", err)
		select {
		case <-ctx.Done():
			nc.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return &NATS{nc: nc, js: js}, nil
}

// Publish implements Bus.
func (n *NATS) Publish(ctx context.Context, subject, msgID string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode %s message: %w", subject, err)
	}
	var opts []jetstream.PublishOpt
	if msgID != "" {
		// Duplicate detection is per stream, and every subject shares the ESM stream; scope the
		// ID to the subject so e.g. an incident and its masked version are not seen as duplicates.
		opts = append(opts, jetstream.WithMsgID(subject+":"+msgID))
	}
	ack, err := n.js.Publish(ctx, subject, data, opts...)
	if err == nil && ack.Duplicate {
		slog.Info("duplicate publish ignored by JetStream", "subject", subject, "msg_id", msgID)
	}
	return err
}

// Subscribe implements Bus. Failed messages are redelivered; after maxDeliver attempts they go to esm.dlq.
func (n *NATS) Subscribe(ctx context.Context, subject, durable string, h Handler) error {
	cons, err := n.js.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       durable,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Minute,
		MaxDeliver:    maxDeliver,
	})
	if err != nil {
		return fmt.Errorf("consumer %s: %w", durable, err)
	}
	cc, err := cons.Consume(func(m jetstream.Msg) {
		herr := h(ctx, m.Data())
		if herr == nil {
			_ = m.Ack()
			return
		}
		meta, _ := m.Metadata()
		if meta != nil && meta.NumDelivered >= maxDeliver {
			slog.Error("message failed permanently, sending to DLQ", "subject", subject, "error", herr)
			dlq := map[string]any{"subject": subject, "consumer": durable, "error": herr.Error(), "data": json.RawMessage(m.Data())}
			if err := n.Publish(ctx, contracts.SubjectDLQ, "", dlq); err != nil {
				slog.Error("DLQ publish failed", "error", err)
			}
			_ = m.Term()
			return
		}
		slog.Warn("message failed, will retry", "subject", subject, "error", herr)
		_ = m.NakWithDelay(5 * time.Second)
	})
	if err != nil {
		return err
	}
	<-ctx.Done()
	cc.Stop()
	return ctx.Err()
}

// KeyValue returns the JetStream key-value bucket name, creating it if needed. Entries expire
// after the stream's MaxAge, so state is never kept longer than the messages it came from.
func (n *NATS) KeyValue(ctx context.Context, bucket string) (jetstream.KeyValue, error) {
	kv, err := n.js.CreateOrUpdateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:  bucket,
		History: 1,
		Storage: jetstream.FileStorage,
		TTL:     streamMaxAge,
	})
	if err != nil {
		return nil, fmt.Errorf("key-value bucket %s: %w", bucket, err)
	}
	return kv, nil
}

// Close implements Bus.
func (n *NATS) Close() error {
	return n.nc.Drain()
}

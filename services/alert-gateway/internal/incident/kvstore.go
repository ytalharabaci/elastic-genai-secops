package incident

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

var errUndecodable = errors.New("undecodable correlation window")

// KVStore keeps correlation windows in a NATS JetStream key-value bucket, so they survive a
// restart and are shared by every alert-gateway replica (ADR-027).
type KVStore struct {
	kv jetstream.KeyValue
}

// NewKVStore returns a Store backed by kv.
func NewKVStore(kv jetstream.KeyValue) *KVStore {
	return &KVStore{kv: kv}
}

// kvKey maps a correlation key (host|user|technique, any characters) to a valid KV key.
func kvKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "w." + hex.EncodeToString(sum[:16])
}

// Get implements Store.
func (s *KVStore) Get(ctx context.Context, key string) (*Window, uint64, error) {
	return s.get(ctx, kvKey(key))
}

// Create implements Store.
func (s *KVStore) Create(ctx context.Context, key string, w *Window) (uint64, error) {
	data, err := json.Marshal(w)
	if err != nil {
		return 0, err
	}
	rev, err := s.kv.Create(ctx, kvKey(key), data)
	return rev, mapConflict(err)
}

// Update implements Store.
func (s *KVStore) Update(ctx context.Context, key string, w *Window, rev uint64) (uint64, error) {
	data, err := json.Marshal(w)
	if err != nil {
		return 0, err
	}
	next, err := s.kv.Update(ctx, kvKey(key), data, rev)
	return next, mapConflict(err)
}

// Delete implements Store. The delete marker it leaves expires with the bucket's MaxAge.
func (s *KVStore) Delete(ctx context.Context, key string, rev uint64) error {
	return mapConflict(s.kv.Delete(ctx, kvKey(key), jetstream.LastRevision(rev)))
}

// Keys implements Store. It returns the correlation keys of the stored windows.
func (s *KVStore) Keys(ctx context.Context) ([]string, error) {
	lister, err := s.kv.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	var keys []string
	for k := range lister.Keys() {
		w, _, err := s.get(ctx, k)
		if errors.Is(err, ErrNotFound) || errors.Is(err, errUndecodable) {
			continue // finished since it was listed; an unreadable entry expires with MaxAge
		}
		if err != nil {
			_ = lister.Stop()
			return nil, err
		}
		keys = append(keys, w.Incident.CorrelationKey)
	}
	return keys, nil
}

func (s *KVStore) get(ctx context.Context, kvk string) (*Window, uint64, error) {
	e, err := s.kv.Get(ctx, kvk)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	var w Window
	if err := json.Unmarshal(e.Value(), &w); err != nil || w.Incident == nil {
		return nil, 0, fmt.Errorf("%w %s: %v", errUndecodable, kvk, err)
	}
	return &w, e.Revision(), nil
}

// mapConflict turns JetStream's compare-and-set failures into ErrConflict.
func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, jetstream.ErrKeyExists) || errors.Is(err, jetstream.ErrKeyRevisionMismatch) {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequence ||
		apiErr.ErrorCode == jetstream.JSErrCodeStreamWrongLastSequenceConstant) {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return err
}

// alert-gateway correlates alerts from esm.alerts into incidents on esm.incidents.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"esm/services/alert-gateway/internal/incident"

	"esm/libs/go-common/bus"
	"esm/libs/go-common/contracts"
	"esm/libs/go-common/envx"
)

func main() {
	log := envx.Logger("alert-gateway")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b, err := bus.FromEnv(ctx)
	if err != nil {
		log.Error("bus connect", "error", err)
		os.Exit(1)
	}
	defer b.Close()

	store, err := windowStore(ctx, b)
	if err != nil {
		log.Error("correlation store", "error", err)
		os.Exit(1)
	}

	// The incident ID is the message ID, so publishing a window twice is deduplicated.
	publish := func(ctx context.Context, inc *contracts.Incident) error {
		if err := b.Publish(ctx, contracts.SubjectIncidents, inc.ID, inc); err != nil {
			return err
		}
		log.Info("incident published", "incident_id", inc.ID, "alerts", inc.AlertCount, "risk", inc.RiskScore)
		return nil
	}

	threshold := envx.Int("CORRELATION_THRESHOLD", 5)
	window := envx.Duration("CORRELATION_WINDOW", 10*time.Minute)
	correlator := incident.NewCorrelator(store, publish, threshold, window)

	go flushLoop(ctx, log, correlator)

	log.Info("alert-gateway started", "threshold", threshold, "window", window.String())
	err = b.Subscribe(ctx, contracts.SubjectAlerts, "alert-gateway", func(ctx context.Context, data []byte) error {
		var a contracts.Alert
		if err := json.Unmarshal(data, &a); err != nil {
			log.Error("invalid alert message, dropping", "error", err)
			return nil
		}
		if err := correlator.Ingest(ctx, &a); err != nil {
			return fmt.Errorf("correlate alert %s: %w", a.ID, err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("alert-gateway stopped with error", "error", err)
		os.Exit(1)
	}
	log.Info("alert-gateway stopped")
}

// windowStore keeps open correlation windows in the NATS key-value bucket esm-correlation, so
// they survive restarts and replicas share them (ADR-027).
func windowStore(ctx context.Context, b bus.Bus) (incident.Store, error) {
	n, ok := b.(*bus.NATS)
	if !ok {
		return nil, fmt.Errorf("bus %T has no key-value store", b)
	}
	kv, err := n.KeyValue(ctx, "esm-correlation")
	if err != nil {
		return nil, err
	}
	return incident.NewKVStore(kv), nil
}

// flushLoop publishes incidents whose correlation window expired before reaching the threshold.
// Every replica runs it; compare-and-set on the store lets only one of them close a window.
func flushLoop(ctx context.Context, log *slog.Logger, c *incident.Correlator) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Flush(ctx); err != nil && ctx.Err() == nil {
				log.Error("correlation flush", "error", err)
			}
		}
	}
}

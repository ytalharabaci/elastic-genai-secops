package incident

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"esm/libs/go-common/bus"
	"esm/libs/go-common/contracts"
)

// Integration tests against a real NATS server; they run only when NATS_TEST_URL is set, e.g.
//
//	NATS_TEST_URL=nats://localhost:4222 go test ./services/alert-gateway/internal/incident/
func kvStore(t *testing.T, bucket string) *KVStore {
	t.Helper()
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	n, err := bus.NewNATS(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	kv, err := n.KeyValue(ctx, bucket)
	if err != nil {
		t.Fatal(err)
	}
	return NewKVStore(kv)
}

func TestKVStoreCompareAndSet(t *testing.T) {
	s := kvStore(t, "esm-test-cas-"+time.Now().Format("150405000000"))
	ctx := context.Background()
	key := "PC-01|alice@corp|T1059" // characters that are not valid in a KV key
	w := &Window{Incident: &contracts.Incident{ID: "inc-1", CorrelationKey: key}, ExpiresAt: time.Now()}

	if _, _, err := s.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty bucket: %v", err)
	}
	rev, err := s.Create(ctx, key, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, key, w); !errors.Is(err, ErrConflict) {
		t.Fatalf("second Create: %v", err)
	}
	next, err := s.Update(ctx, key, w, rev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, key, w, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("Update with stale revision: %v", err)
	}
	if keys, err := s.Keys(ctx); err != nil || len(keys) != 1 || keys[0] != key {
		t.Fatalf("Keys = %v, %v", keys, err)
	}
	if err := s.Delete(ctx, key, rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("Delete with stale revision: %v", err)
	}
	if err := s.Delete(ctx, key, next); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if keys, err := s.Keys(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("Keys after Delete = %v, %v", keys, err)
	}
	if _, err := s.Create(ctx, key, w); err != nil {
		t.Fatalf("Create after Delete: %v", err)
	}
}

func TestKVReplicasShareWindows(t *testing.T) {
	bucket := "esm-test-replicas-" + time.Now().Format("150405000000")
	const alerts, threshold = 60, 4
	rec := &recorder{}
	replicas := []*Correlator{
		NewCorrelator(kvStore(t, bucket), rec.publish, threshold, time.Hour),
		NewCorrelator(kvStore(t, bucket), rec.publish, threshold, time.Hour),
	}
	var wg sync.WaitGroup
	errs := make(chan error, alerts)
	for r, c := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := r; i < alerts; i += len(replicas) {
				errs <- c.Ingest(context.Background(), alert(fmt.Sprintf("a%d", i), "h1", "u1", "low", "T1059"))
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, inc := range rec.incidents() {
		for _, a := range inc.Alerts {
			if seen[a.ID] {
				t.Fatalf("alert %s in two incidents", a.ID)
			}
			seen[a.ID] = true
		}
	}
	if len(seen) != alerts {
		t.Fatalf("%d of %d alerts published", len(seen), alerts)
	}
}

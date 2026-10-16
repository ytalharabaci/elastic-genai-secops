package incident

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"esm/libs/go-common/contracts"
)

func alert(id, host, user, sev, technique string) *contracts.Alert {
	return &contracts.Alert{ID: id, Timestamp: time.Now(), HostName: host, UserName: user, Severity: sev,
		MITRETechniqueIDs: []string{technique}}
}

// recorder is a Publisher that keeps the published incidents, deduplicated by ID as JetStream
// does with the message ID.
type recorder struct {
	mu   sync.Mutex
	fail error
	got  []*contracts.Incident
	seen map[string]bool
}

func (r *recorder) publish(_ context.Context, inc *contracts.Incident) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	if r.seen == nil {
		r.seen = map[string]bool{}
	}
	if !r.seen[inc.ID] {
		r.seen[inc.ID] = true
		r.got = append(r.got, inc)
	}
	return nil
}

func (r *recorder) incidents() []*contracts.Incident {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*contracts.Incident(nil), r.got...)
}

func newTest(threshold int, window time.Duration) (*Correlator, *recorder, *MemoryStore) {
	rec, store := &recorder{}, NewMemoryStore()
	return NewCorrelator(store, rec.publish, threshold, window), rec, store
}

func ingest(t *testing.T, c *Correlator, a *contracts.Alert) {
	t.Helper()
	if err := c.Ingest(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}

func TestThresholdEmitsIncident(t *testing.T) {
	c, rec, store := newTest(2, time.Hour)
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	if len(rec.incidents()) != 0 {
		t.Fatal("incident emitted before threshold")
	}
	ingest(t, c, alert("a2", "h1", "u1", "medium", "T1059"))
	got := rec.incidents()
	if len(got) != 1 {
		t.Fatal("expected incident at threshold")
	}
	if inc := got[0]; inc.AlertCount != 2 || inc.RiskScore != 35 || inc.Status != StatusPendingTriage {
		t.Fatalf("unexpected incident: count=%d risk=%d status=%s", inc.AlertCount, inc.RiskScore, inc.Status)
	}
	if keys, _ := store.Keys(context.Background()); len(keys) != 0 {
		t.Fatalf("published window not removed: %v", keys)
	}
}

func TestThresholdOneEmitsOnFirstAlert(t *testing.T) {
	c, rec, _ := newTest(0, time.Hour)
	ingest(t, c, alert("a1", "h1", "u1", "low", "T1059"))
	if len(rec.incidents()) != 1 {
		t.Fatal("threshold 1 must emit on the first alert")
	}
}

func TestDifferentKeysAreSeparate(t *testing.T) {
	c, rec, _ := newTest(2, time.Hour)
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	ingest(t, c, alert("a2", "h2", "u1", "high", "T1059"))
	if len(rec.incidents()) != 0 {
		t.Fatal("alerts on different hosts must not be correlated")
	}
}

func TestDuplicateAlertIgnored(t *testing.T) {
	c, rec, _ := newTest(2, time.Hour)
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	if len(rec.incidents()) != 0 {
		t.Fatal("redelivered alert must not count twice")
	}
}

func TestFlushAfterWindow(t *testing.T) {
	now := time.Now()
	c, rec, _ := newTest(10, time.Minute)
	c.now = func() time.Time { return now }
	ingest(t, c, alert("a1", "h1", "u1", "low", "T1110"))
	if err := c.Flush(context.Background()); err != nil || len(rec.incidents()) != 0 {
		t.Fatalf("flushed before the window expired (err=%v)", err)
	}
	now = now.Add(2 * time.Minute)
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := rec.incidents()
	if len(got) != 1 || got[0].AlertCount != 1 || got[0].Status != StatusPendingTriage {
		t.Fatalf("expected one pending incident, got %+v", got)
	}
}

func TestWindowSurvivesRestart(t *testing.T) {
	rec, store := &recorder{}, NewMemoryStore()
	first := NewCorrelator(store, rec.publish, 3, time.Hour)
	ingest(t, first, alert("a1", "h1", "u1", "high", "T1059"))
	ingest(t, first, alert("a2", "h1", "u1", "high", "T1059"))

	restarted := NewCorrelator(store, rec.publish, 3, time.Hour)
	ingest(t, restarted, alert("a3", "h1", "u1", "high", "T1059"))
	got := rec.incidents()
	if len(got) != 1 || got[0].AlertCount != 3 {
		t.Fatalf("window lost across restart: %+v", got)
	}
}

func TestPublishFailureKeepsWindowClosed(t *testing.T) {
	c, rec, store := newTest(2, time.Hour)
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	rec.fail = errors.New("bus down")
	// a2 is stored in the closed window, so it is acknowledged even though publishing failed.
	ingest(t, c, alert("a2", "h1", "u1", "high", "T1059"))
	if len(rec.incidents()) != 0 {
		t.Fatal("nothing can be published while the bus is down")
	}
	rec.fail = nil

	// A new alert for the same key finishes the closed window first, then opens a new one.
	ingest(t, c, alert("a3", "h1", "u1", "high", "T1059"))
	got := rec.incidents()
	if len(got) != 1 || got[0].AlertCount != 2 {
		t.Fatalf("closed window not published: %+v", got)
	}
	w, _, err := store.Get(context.Background(), correlationKey(alert("", "h1", "u1", "", "T1059")))
	if err != nil || w.Incident.AlertCount != 1 || !hasAlert(w.Incident, "a3") {
		t.Fatalf("expected a new window with a3, got %+v (err=%v)", w, err)
	}
}

func TestClosedWindowBlocksUntilPublished(t *testing.T) {
	c, rec, _ := newTest(1, time.Hour)
	rec.fail = errors.New("bus down")
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	// The key is taken by an unpublished window: a new alert must wait for redelivery.
	if err := c.Ingest(context.Background(), alert("a2", "h1", "u1", "high", "T1059")); err == nil {
		t.Fatal("expected an error while the closed window cannot be published")
	}
	// The window's own alert is acknowledged once it is published.
	rec.fail = nil
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	if got := rec.incidents(); len(got) != 1 || got[0].AlertCount != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestFlushFinishesClosedWindow(t *testing.T) {
	c, rec, _ := newTest(1, time.Hour)
	rec.fail = errors.New("bus down")
	ingest(t, c, alert("a1", "h1", "u1", "high", "T1059"))
	if err := c.Flush(context.Background()); err == nil {
		t.Fatal("flush must report the failed publish")
	}
	rec.fail = nil
	if err := c.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.incidents(); len(got) != 1 {
		t.Fatalf("flush must publish a window left closed, got %d", len(got))
	}
}

func TestReplicasShareWindows(t *testing.T) {
	const alerts, threshold = 200, 5
	rec, store := &recorder{}, NewMemoryStore()
	replicas := []*Correlator{
		NewCorrelator(store, rec.publish, threshold, time.Hour),
		NewCorrelator(store, rec.publish, threshold, time.Hour),
		NewCorrelator(store, rec.publish, threshold, time.Hour),
	}
	// Each replica consumes its share of the alerts in order, as a JetStream consumer does.
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
		if inc.AlertCount != threshold {
			t.Fatalf("incident %s has %d alerts, want %d", inc.ID, inc.AlertCount, threshold)
		}
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

func TestRiskScoreCapped(t *testing.T) {
	c, rec, _ := newTest(5, time.Hour)
	for _, id := range []string{"a1", "a2", "a3", "a4", "a5"} {
		ingest(t, c, alert(id, "h1", "u1", "critical", "T1003"))
	}
	got := rec.incidents()
	if len(got) != 1 || got[0].RiskScore != 100 {
		t.Fatalf("risk score should cap at 100, got %+v", got)
	}
}

func TestAddUniqueSortedAndSkipsEmpty(t *testing.T) {
	var s []string
	for _, v := range []string{"b", "a", "", "b", "c"} {
		s = addUnique(s, v)
	}
	if len(s) != 3 || s[0] != "a" || s[1] != "b" || s[2] != "c" {
		t.Fatalf("got %v", s)
	}
}

// Package incident groups related alerts into incidents (the only place incidents are formed).
package incident

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"esm/libs/go-common/contracts"
)

// Incident statuses.
const (
	StatusNew           = "new"
	StatusPendingTriage = "pending_triage"
)

// severityWeight maps severity to its contribution to the risk score.
var severityWeight = map[string]int{
	"critical": 40,
	"high":     25,
	"medium":   10,
	"low":      3,
}

// Publisher sends a finished incident to esm.incidents. It must be idempotent per incident ID:
// a window can be published twice when a replica stops between publishing and deleting it.
type Publisher func(ctx context.Context, inc *contracts.Incident) error

// maxAttempts bounds the compare-and-set retries of one Ingest under contention.
const maxAttempts = 16

// Correlator groups alerts by (host, user, first MITRE technique) inside a time window.
// An incident is published when it reaches the alert threshold or its window expires.
// Windows live in a Store, so they survive restarts and can be shared by replicas (ADR-027).
type Correlator struct {
	store     Store
	publish   Publisher
	threshold int
	window    time.Duration
	now       func() time.Time
}

// NewCorrelator returns a Correlator. threshold < 1 is treated as 1.
func NewCorrelator(store Store, publish Publisher, threshold int, window time.Duration) *Correlator {
	if threshold < 1 {
		threshold = 1
	}
	return &Correlator{store: store, publish: publish, threshold: threshold, window: window, now: time.Now}
}

// Ingest adds an alert to its window and publishes the incident once the window is complete.
// It returns nil once the alert is stored in a window (or already was); any error means the
// alert must be redelivered.
func (c *Correlator) Ingest(ctx context.Context, a *contracts.Alert) error {
	key := correlationKey(a)
	for range maxAttempts {
		done, err := c.tryIngest(ctx, key, a)
		if errors.Is(err, ErrConflict) {
			continue // another replica changed the window; read it again
		}
		if err != nil || done {
			return err
		}
	}
	return fmt.Errorf("correlation window %q: too many concurrent updates", key)
}

func (c *Correlator) tryIngest(ctx context.Context, key string, a *contracts.Alert) (bool, error) {
	w, rev, err := c.store.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		w = &Window{
			Incident: &contracts.Incident{
				ID:             newID(),
				CreatedAt:      a.Timestamp,
				Status:         StatusNew,
				CorrelationKey: key,
			},
			ExpiresAt: c.now().Add(c.window),
		}
		add(w.Incident, a)
		if rev, err = c.store.Create(ctx, key, w); err != nil {
			return false, err
		}
		return true, c.closeIfComplete(ctx, key, w, rev)
	}
	if err != nil {
		return false, err
	}
	if !w.ClosedAt.IsZero() {
		// Being published by another replica, or by one that stopped midway: finish it, then
		// start a new window unless the alert was part of this one.
		if err := c.finish(ctx, key, w, rev); err != nil {
			return false, err
		}
		return hasAlert(w.Incident, a.ID), nil
	}
	if hasAlert(w.Incident, a.ID) {
		return true, nil // redelivered alert
	}
	add(w.Incident, a)
	if c.complete(w) {
		return true, c.close(ctx, key, w, rev)
	}
	_, err = c.store.Update(ctx, key, w, rev)
	return err == nil, err
}

// Flush publishes every window that has expired, and finishes windows left closed by a replica
// that stopped while publishing. It continues past failures and returns them joined.
func (c *Correlator) Flush(ctx context.Context) error {
	keys, err := c.store.Keys(ctx)
	if err != nil {
		return fmt.Errorf("list correlation windows: %w", err)
	}
	var errs []error
	for _, key := range keys {
		w, rev, err := c.store.Get(ctx, key)
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			errs = append(errs, err)
		case !w.ClosedAt.IsZero():
			errs = append(errs, c.finish(ctx, key, w, rev))
		case c.now().After(w.ExpiresAt):
			if err := c.close(ctx, key, w, rev); !errors.Is(err, ErrConflict) {
				errs = append(errs, err) // on a conflict the window is retried next flush
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Correlator) complete(w *Window) bool {
	return w.Incident.AlertCount >= c.threshold || c.now().After(w.ExpiresAt)
}

func (c *Correlator) closeIfComplete(ctx context.Context, key string, w *Window, rev uint64) error {
	if !c.complete(w) {
		return nil
	}
	return c.close(ctx, key, w, rev)
}

// close marks the window as being published, so no replica adds to it, then publishes it.
// Once the closed window is stored its alerts are safe: a failed publish is logged and left to
// the next Ingest or Flush, so the alert is not redelivered into a second window.
func (c *Correlator) close(ctx context.Context, key string, w *Window, rev uint64) error {
	w.ClosedAt = c.now()
	w.Incident.Status = StatusPendingTriage
	rev, err := c.store.Update(ctx, key, w, rev)
	if err != nil {
		return err
	}
	if err := c.finish(ctx, key, w, rev); err != nil {
		slog.Warn("incident publish deferred, the window stays closed", "incident_id", w.Incident.ID, "error", err)
	}
	return nil
}

// finish publishes a closed window and removes it. If publishing fails the window stays closed
// and is published again by the next Ingest or Flush that finds it.
func (c *Correlator) finish(ctx context.Context, key string, w *Window, rev uint64) error {
	if err := c.publish(ctx, w.Incident); err != nil {
		return fmt.Errorf("publish incident %s: %w", w.Incident.ID, err)
	}
	if err := c.store.Delete(ctx, key, rev); err != nil && !errors.Is(err, ErrConflict) {
		return fmt.Errorf("delete window of incident %s: %w", w.Incident.ID, err)
	}
	return nil // on a conflict another replica finished it first
}

func hasAlert(inc *contracts.Incident, id string) bool {
	for _, a := range inc.Alerts {
		if a.ID == id {
			return true
		}
	}
	return false
}

// add appends a to the incident and updates its counters, entities and risk score.
func add(inc *contracts.Incident, a *contracts.Alert) {
	inc.Alerts = append(inc.Alerts, a)
	inc.AlertCount = len(inc.Alerts)
	if a.Timestamp.After(inc.UpdatedAt) {
		inc.UpdatedAt = a.Timestamp
	}
	if a.Timestamp.Before(inc.CreatedAt) {
		inc.CreatedAt = a.Timestamp
	}
	merge(inc, a)
	inc.RiskScore = riskScore(inc)
}

func correlationKey(a *contracts.Alert) string {
	technique := ""
	if len(a.MITRETechniqueIDs) > 0 {
		technique = a.MITRETechniqueIDs[0]
	}
	return fmt.Sprintf("%s|%s|%s", a.HostName, a.UserName, technique)
}

func merge(inc *contracts.Incident, a *contracts.Alert) {
	inc.AffectedHosts = addUnique(inc.AffectedHosts, a.HostName)
	inc.AffectedUsers = addUnique(inc.AffectedUsers, a.UserName)
	inc.SourceIPs = addUnique(inc.SourceIPs, a.SourceIP)
	for _, t := range a.MITRETechniqueIDs {
		inc.MITRETechniques = addUnique(inc.MITRETechniques, t)
	}
}

func riskScore(inc *contracts.Incident) int {
	score := 0
	for _, a := range inc.Alerts {
		score += severityWeight[a.Severity]
	}
	if n := len(inc.AffectedHosts); n > 1 {
		score += (n - 1) * 5
	}
	if n := len(inc.MITRETechniques); n > 1 {
		score += (n - 1) * 3
	}
	if score > 100 {
		score = 100
	}
	return score
}

// addUnique appends v (if non-empty and not present) and keeps the slice sorted.
func addUnique(ss []string, v string) []string {
	if v == "" {
		return ss
	}
	i := sort.SearchStrings(ss, v)
	if i < len(ss) && ss[i] == v {
		return ss
	}
	ss = append(ss, "")
	copy(ss[i+1:], ss[i:])
	ss[i] = v
	return ss
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "inc-" + hex.EncodeToString(b)
}

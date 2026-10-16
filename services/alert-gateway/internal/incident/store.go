package incident

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"esm/libs/go-common/contracts"
)

// Store errors.
var (
	ErrNotFound = errors.New("correlation window not found")
	ErrConflict = errors.New("correlation window changed concurrently")
)

// Window is one open correlation window. A non-zero ClosedAt means the incident is being
// published: nothing is added to it, and whoever finds it finishes the publication (ADR-027).
type Window struct {
	Incident  *contracts.Incident `json:"incident"`
	ExpiresAt time.Time           `json:"expires_at"`
	ClosedAt  time.Time           `json:"closed_at,omitzero"`
}

// Store keeps correlation windows by correlation key. Writes are compare-and-set on the
// revision returned by Get, Create or Update, so several alert-gateway replicas can share it.
type Store interface {
	// Get returns ErrNotFound when no window exists for key.
	Get(ctx context.Context, key string) (*Window, uint64, error)
	// Create returns ErrConflict when a window already exists for key.
	Create(ctx context.Context, key string, w *Window) (uint64, error)
	// Update returns ErrConflict when the stored revision is not rev.
	Update(ctx context.Context, key string, w *Window, rev uint64) (uint64, error)
	// Delete returns ErrConflict when the stored revision is not rev.
	Delete(ctx context.Context, key string, rev uint64) error
	Keys(ctx context.Context) ([]string, error)
}

// MemoryStore is a Store for one process; windows are lost on restart.
type MemoryStore struct {
	mu      sync.Mutex
	rev     uint64
	windows map[string]memoryEntry
}

type memoryEntry struct {
	w   Window
	rev uint64
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{windows: map[string]memoryEntry{}}
}

// Get implements Store. The window is a copy; callers change it and write it back.
func (s *MemoryStore) Get(_ context.Context, key string) (*Window, uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.windows[key]
	if !ok {
		return nil, 0, ErrNotFound
	}
	return cloneWindow(e.w), e.rev, nil
}

// Create implements Store.
func (s *MemoryStore) Create(_ context.Context, key string, w *Window) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.windows[key]; ok {
		return 0, ErrConflict
	}
	return s.put(key, w), nil
}

// Update implements Store.
func (s *MemoryStore) Update(_ context.Context, key string, w *Window, rev uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.windows[key]; !ok || e.rev != rev {
		return 0, ErrConflict
	}
	return s.put(key, w), nil
}

// Delete implements Store.
func (s *MemoryStore) Delete(_ context.Context, key string, rev uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.windows[key]; !ok || e.rev != rev {
		return ErrConflict
	}
	delete(s.windows, key)
	return nil
}

// Keys implements Store.
func (s *MemoryStore) Keys(_ context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.windows))
	for k := range s.windows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *MemoryStore) put(key string, w *Window) uint64 {
	s.rev++
	s.windows[key] = memoryEntry{w: *cloneWindow(*w), rev: s.rev}
	return s.rev
}

// cloneWindow copies the incident and its slices so stored state is not shared with callers.
// Alerts are not modified after ingest and are shared.
func cloneWindow(w Window) *Window {
	if w.Incident != nil {
		inc := *w.Incident
		inc.Alerts = append([]*contracts.Alert(nil), inc.Alerts...)
		inc.AffectedHosts = append([]string(nil), inc.AffectedHosts...)
		inc.AffectedUsers = append([]string(nil), inc.AffectedUsers...)
		inc.SourceIPs = append([]string(nil), inc.SourceIPs...)
		inc.MITRETechniques = append([]string(nil), inc.MITRETechniques...)
		w.Incident = &inc
	}
	return &w
}

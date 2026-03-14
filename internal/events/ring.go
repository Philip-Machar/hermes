package events

import (
	"sync"
	"time"
)

// Kind classifies an event for the dashboard.
type Kind string

const (
	KindDirect  Kind = "direct"
	KindQueued  Kind = "queued"
	KindError   Kind = "error"
	KindInfo    Kind = "info"
)

// Event is a single activity record.
type Event struct {
	Time      time.Time `json:"time"`
	JobID     string    `json:"job_id"`
	JobType   string    `json:"job_type"`
	Kind      Kind      `json:"kind"`
	WorkerID  string    `json:"worker_id,omitempty"`
	Message   string    `json:"message"`
}

// Ring is a fixed-capacity thread-safe ring buffer of Events.
// Oldest entries are overwritten when the buffer is full.
type Ring struct {
	mu       sync.Mutex
	buf      []Event
	cap      int
	head     int  // index of next write
	count    int  // number of valid entries
}

// NewRing returns a Ring with the given capacity.
func NewRing(capacity int) *Ring {
	return &Ring{
		buf: make([]Event, capacity),
		cap: capacity,
	}
}

// Push adds an event to the ring, overwriting the oldest entry if full.
func (r *Ring) Push(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap
	if r.count < r.cap {
		r.count++
	}
}

// Snapshot returns all events in chronological order (oldest first).
func (r *Ring) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Event, r.count)
	if r.count == 0 {
		return out
	}

	// If buffer is not yet full, entries start at index 0.
	// If full, the oldest entry is at r.head.
	start := 0
	if r.count == r.cap {
		start = r.head
	}
	for i := 0; i < r.count; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}
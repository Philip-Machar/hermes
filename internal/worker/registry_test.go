package worker

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister_StoresWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("worker-1", "localhost:9001")

	snapshot := r.List()
	info, ok := snapshot["worker-1"]
	if !ok {
		t.Fatal("expected worker-1 to be in registry")
	}
	if info.Address != "localhost:9001" {
		t.Errorf("expected address localhost:9001, got %s", info.Address)
	}
	if info.Load != 0 {
		t.Errorf("expected initial load 0, got %d", info.Load)
	}
}

func TestRegister_OverwritesExistingWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("worker-1", "localhost:9001")
	r.Register("worker-1", "localhost:9002") // re-register with new address

	snapshot := r.List()
	if snapshot["worker-1"].Address != "localhost:9002" {
		t.Errorf("expected updated address localhost:9002, got %s", snapshot["worker-1"].Address)
	}
}

// ---------------------------------------------------------------------------
// UpdateLoad
// ---------------------------------------------------------------------------

func TestUpdateLoad_UpdatesKnownWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("worker-1", "localhost:9001")
	r.UpdateLoad("worker-1", 5)

	snapshot := r.List()
	if snapshot["worker-1"].Load != 5 {
		t.Errorf("expected load 5, got %d", snapshot["worker-1"].Load)
	}
}

func TestUpdateLoad_RefreshesLastSeen(t *testing.T) {
	r := NewRegistry()
	r.Register("worker-1", "localhost:9001")
	before := r.List()["worker-1"].LastSeen

	time.Sleep(5 * time.Millisecond)
	r.UpdateLoad("worker-1", 1)

	after := r.List()["worker-1"].LastSeen
	if !after.After(before) {
		t.Error("expected LastSeen to be refreshed after UpdateLoad")
	}
}

func TestUpdateLoad_UnknownWorkerIsNoop(t *testing.T) {
	r := NewRegistry()
	// Should not panic
	r.UpdateLoad("ghost", 10)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestList_ReturnsSnapshot(t *testing.T) {
	r := NewRegistry()
	r.Register("w1", "localhost:9001")
	r.Register("w2", "localhost:9002")

	snapshot := r.List()
	if len(snapshot) != 2 {
		t.Errorf("expected 2 workers, got %d", len(snapshot))
	}
}

func TestList_SnapshotIsIndependent(t *testing.T) {
	r := NewRegistry()
	r.Register("w1", "localhost:9001")

	snapshot := r.List()
	// Mutating the snapshot should not affect the registry.
	delete(snapshot, "w1")

	snapshot2 := r.List()
	if _, ok := snapshot2["w1"]; !ok {
		t.Error("deleting from snapshot should not affect the registry")
	}
}

// ---------------------------------------------------------------------------
// PickWorker
// ---------------------------------------------------------------------------

func TestPickWorker_ReturnsEmptyWhenNoWorkers(t *testing.T) {
	r := NewRegistry()
	if id := r.PickWorker(); id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}

func TestPickWorker_ReturnsSingleWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("w1", "localhost:9001")

	if id := r.PickWorker(); id != "w1" {
		t.Errorf("expected w1, got %q", id)
	}
}

func TestPickWorker_PrefersLowestLoad(t *testing.T) {
	r := NewRegistry()
	r.Register("busy", "localhost:9001")
	r.Register("idle", "localhost:9002")

	r.UpdateLoad("busy", 10)
	r.UpdateLoad("idle", 0)

	if id := r.PickWorker(); id != "idle" {
		t.Errorf("expected idle worker to be picked, got %q", id)
	}
}

// ---------------------------------------------------------------------------
// CleanupExpired
// ---------------------------------------------------------------------------

func TestCleanupExpired_RemovesStaleWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("stale", "localhost:9001")

	// Manually backdate LastSeen past the TTL.
	r.mu.Lock()
	r.workers["stale"].LastSeen = time.Now().Add(-(workerTTL + time.Second))
	r.mu.Unlock()

	r.CleanupExpired()

	snapshot := r.List()
	if _, ok := snapshot["stale"]; ok {
		t.Error("expected stale worker to be removed")
	}
}

func TestCleanupExpired_KeepsFreshWorker(t *testing.T) {
	r := NewRegistry()
	r.Register("fresh", "localhost:9001")

	r.CleanupExpired()

	snapshot := r.List()
	if _, ok := snapshot["fresh"]; !ok {
		t.Error("expected fresh worker to remain in registry")
	}
}

func TestCleanupExpired_OnlyRemovesExpired(t *testing.T) {
	r := NewRegistry()
	r.Register("stale", "localhost:9001")
	r.Register("fresh", "localhost:9002")

	r.mu.Lock()
	r.workers["stale"].LastSeen = time.Now().Add(-(workerTTL + time.Second))
	r.mu.Unlock()

	r.CleanupExpired()

	snapshot := r.List()
	if _, ok := snapshot["stale"]; ok {
		t.Error("stale worker should have been removed")
	}
	if _, ok := snapshot["fresh"]; !ok {
		t.Error("fresh worker should still be present")
	}
}

// ---------------------------------------------------------------------------
// DispatchJob
// ---------------------------------------------------------------------------

func TestDispatchJob_SucceedsWhenWorkerResponds200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/dispatch" {
			t.Errorf("expected /dispatch, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := NewRegistry()
	// Strip the "http://" prefix — registry prepends it internally.
	r.Register("w1", server.Listener.Addr().String())

	err := r.DispatchJob("w1", "job-1", "email", map[string]string{"to": "a@b.com"})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDispatchJob_FailsWhenWorkerRespondsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	r := NewRegistry()
	r.Register("w1", server.Listener.Addr().String())

	err := r.DispatchJob("w1", "job-1", "email", nil)
	if err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestDispatchJob_FailsForUnknownWorker(t *testing.T) {
	r := NewRegistry()
	err := r.DispatchJob("ghost", "job-1", "email", nil)
	if err == nil {
		t.Error("expected error for unknown worker")
	}
}

func TestDispatchJob_FailsWhenWorkerHasNoAddress(t *testing.T) {
	r := NewRegistry()
	r.Register("w1", "") // no address

	err := r.DispatchJob("w1", "job-1", "email", nil)
	if err == nil {
		t.Error("expected error when worker has no address")
	}
}

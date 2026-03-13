package worker

import (
	"context"
	"testing"

	"jobqueue/proto/workerpb"
)

// ---------------------------------------------------------------------------
// splitWorkerID
// ---------------------------------------------------------------------------

func TestSplitWorkerID_WithAt(t *testing.T) {
	id, addr := splitWorkerID("abc-123@localhost:9001")
	if id != "abc-123" {
		t.Errorf("expected id abc-123, got %q", id)
	}
	if addr != "localhost:9001" {
		t.Errorf("expected addr localhost:9001, got %q", addr)
	}
}

func TestSplitWorkerID_WithoutAt(t *testing.T) {
	id, addr := splitWorkerID("abc-123")
	if id != "abc-123" {
		t.Errorf("expected id abc-123, got %q", id)
	}
	if addr != "" {
		t.Errorf("expected empty addr, got %q", addr)
	}
}

func TestSplitWorkerID_MultipleAtSigns(t *testing.T) {
	// Only the first @ is the separator; the address itself may contain @.
	id, addr := splitWorkerID("abc@host@9001")
	if id != "abc" {
		t.Errorf("expected id abc, got %q", id)
	}
	if addr != "host@9001" {
		t.Errorf("expected addr host@9001, got %q", addr)
	}
}

// ---------------------------------------------------------------------------
// GRPCServer.Register
// ---------------------------------------------------------------------------

func TestGRPCServer_Register_StoresWorker(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	resp, err := srv.Register(context.Background(), &workerpb.RegisterRequest{
		WorkerId: "worker-1@localhost:9001",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %q", resp.Status)
	}

	snapshot := reg.List()
	info, ok := snapshot["worker-1"]
	if !ok {
		t.Fatal("expected worker-1 in registry after Register")
	}
	if info.Address != "localhost:9001" {
		t.Errorf("expected address localhost:9001, got %q", info.Address)
	}
}

func TestGRPCServer_Register_WithoutAddress(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	resp, err := srv.Register(context.Background(), &workerpb.RegisterRequest{
		WorkerId: "worker-1", // no @ separator
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %q", resp.Status)
	}

	snapshot := reg.List()
	info, ok := snapshot["worker-1"]
	if !ok {
		t.Fatal("expected worker-1 in registry")
	}
	if info.Address != "" {
		t.Errorf("expected empty address, got %q", info.Address)
	}
}

// ---------------------------------------------------------------------------
// GRPCServer.Heartbeat
// ---------------------------------------------------------------------------

func TestGRPCServer_Heartbeat_UpdatesLoad(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	// Register first so there is a worker to heartbeat.
	reg.Register("worker-1", "localhost:9001")

	resp, err := srv.Heartbeat(context.Background(), &workerpb.HeartbeatRequest{
		WorkerId: "worker-1",
		Load:     7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "alive" {
		t.Errorf("expected status alive, got %q", resp.Status)
	}

	snapshot := reg.List()
	if snapshot["worker-1"].Load != 7 {
		t.Errorf("expected load 7, got %d", snapshot["worker-1"].Load)
	}
}

// ---------------------------------------------------------------------------
// GRPCServer.ListWorkers
// ---------------------------------------------------------------------------

func TestGRPCServer_ListWorkers_ReturnsAllWorkers(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	reg.Register("w1", "localhost:9001")
	reg.Register("w2", "localhost:9002")

	resp, err := srv.ListWorkers(context.Background(), &workerpb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(resp.Workers))
	}
}

func TestGRPCServer_ListWorkers_EmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	resp, err := srv.ListWorkers(context.Background(), &workerpb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(resp.Workers))
	}
}

func TestGRPCServer_ListWorkers_ContainsCorrectFields(t *testing.T) {
	reg := NewRegistry()
	srv := NewGRPCServer(reg)

	reg.Register("w1", "localhost:9001")
	reg.UpdateLoad("w1", 3)

	resp, err := srv.ListWorkers(context.Background(), &workerpb.Empty{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(resp.Workers))
	}

	w := resp.Workers[0]
	if w.WorkerId != "w1" {
		t.Errorf("expected worker_id w1, got %q", w.WorkerId)
	}
	if w.Load != 3 {
		t.Errorf("expected load 3, got %d", w.Load)
	}
	if w.LastSeenUnix == 0 {
		t.Error("expected non-zero last_seen_unix")
	}
}

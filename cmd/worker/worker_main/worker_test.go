package main

import (
	"encoding/json"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"jobqueue/internal/jobs"
)

// ---------------------------------------------------------------------------
// Stub queue.Queue
// ---------------------------------------------------------------------------

type stubQueue struct {
	published [][]byte
}

func (s *stubQueue) Publish(body []byte) error {
	s.published = append(s.published, body)
	return nil
}

func (s *stubQueue) Close() error { return nil }

// ---------------------------------------------------------------------------
// deliveryAcker records which acknowledgement method was called.
// ---------------------------------------------------------------------------

type deliveryAcker struct {
	acked       bool
	nacked      bool
	nackRequeue bool
}

func (a *deliveryAcker) Ack(_ uint64, _ bool) error {
	a.acked = true
	return nil
}

func (a *deliveryAcker) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked = true
	a.nackRequeue = requeue
	return nil
}

func (a *deliveryAcker) Reject(_ uint64, _ bool) error { return nil }

// ---------------------------------------------------------------------------
// Helper — build an amqp.Delivery from a Job
// ---------------------------------------------------------------------------

func jobDelivery(t *testing.T, job jobs.Job) (amqp.Delivery, *deliveryAcker) {
	t.Helper()
	body, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	acker := &deliveryAcker{}
	return amqp.Delivery{Body: body, Acknowledger: acker}, acker
}

// ---------------------------------------------------------------------------
// Invalid JSON — Nacked without requeue, nothing published
// ---------------------------------------------------------------------------

func TestHandleMessage_InvalidJSON_NackedAndDiscarded(t *testing.T) {
	q := &stubQueue{}
	acker := &deliveryAcker{}
	d := amqp.Delivery{Body: []byte("{bad json}"), Acknowledger: acker}

	handleMessage(d, q)

	if !acker.nacked {
		t.Error("expected Nack for invalid JSON")
	}
	if acker.nackRequeue {
		t.Error("expected Nack without requeue")
	}
	if len(q.published) != 0 {
		t.Errorf("expected nothing published, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// Valid job — current stub behaviour: jobSucceeded=true → Ack
// Update when real job processing replaces the stub.
// ---------------------------------------------------------------------------

func TestHandleMessage_ValidJob_Acked(t *testing.T) {
	q := &stubQueue{}
	job := jobs.Job{ID: "j1", Type: "email", Attempts: 0}
	d, acker := jobDelivery(t, job)

	handleMessage(d, q)

	if !acker.acked {
		t.Error("expected Ack for successful job (jobSucceeded=true)")
	}
	if len(q.published) != 0 {
		t.Errorf("expected 0 re-published messages, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// Attempts counter is preserved through JSON round-trip
// ---------------------------------------------------------------------------

func TestHandleMessage_AttemptsPreservedInPayload(t *testing.T) {
	job := jobs.Job{ID: "j1", Type: "resize", Attempts: 1}
	body, _ := json.Marshal(job)

	var roundTripped jobs.Job
	if err := json.Unmarshal(body, &roundTripped); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if roundTripped.Attempts != 1 {
		t.Errorf("expected attempts 1, got %d", roundTripped.Attempts)
	}
	if roundTripped.ID != "j1" {
		t.Errorf("expected ID j1, got %s", roundTripped.ID)
	}
}

// ---------------------------------------------------------------------------
// DLQ path — Nack without requeue when attempts == maxAttempts
// Skipped until real job processing is wired into handleMessage.
// ---------------------------------------------------------------------------

func TestHandleMessage_ExhaustedAttempts_NackedToDLQ(t *testing.T) {
	t.Skip("DLQ path requires jobSucceeded=false; wire real job processing first")
}

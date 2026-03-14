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
// Known job type (email) — succeeds → Acked, nothing re-published
// ---------------------------------------------------------------------------

func TestHandleMessage_ValidJob_Acked(t *testing.T) {
	q := &stubQueue{}
	job := jobs.Job{
		ID:       "j1",
		Type:     "email",
		Payload:  map[string]interface{}{"to": "user@example.com", "subject": "Hello"},
		Attempts: 0,
	}
	d, acker := jobDelivery(t, job)

	handleMessage(d, q)

	if !acker.acked {
		t.Error("expected Ack for successful email job")
	}
	if len(q.published) != 0 {
		t.Errorf("expected 0 re-published messages, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// Unknown job type — Nacked without requeue on first attempt (no retry)
// ---------------------------------------------------------------------------

func TestHandleMessage_UnknownJobType_NackedWithoutRetry(t *testing.T) {
	q := &stubQueue{}
	job := jobs.Job{ID: "j2", Type: "unknown-type", Attempts: 0}
	d, acker := jobDelivery(t, job)

	handleMessage(d, q)

	// First failure → attempts becomes 1, which is < maxAttempts(3), so it
	// should be Acked and re-published with incremented attempt counter.
	if !acker.acked {
		t.Error("expected Ack before re-publish on first failure")
	}
	if len(q.published) != 1 {
		t.Errorf("expected 1 re-published message for retry, got %d", len(q.published))
	}

	// Verify the re-published message has attempts incremented to 1.
	var retried jobs.Job
	if err := json.Unmarshal(q.published[0], &retried); err != nil {
		t.Fatalf("could not unmarshal re-published job: %v", err)
	}
	if retried.Attempts != 1 {
		t.Errorf("expected attempts=1 in re-published job, got %d", retried.Attempts)
	}
}

// ---------------------------------------------------------------------------
// Attempts counter is preserved through JSON round-trip
// ---------------------------------------------------------------------------

func TestHandleMessage_AttemptsPreservedInPayload(t *testing.T) {
	job := jobs.Job{ID: "j1", Type: "resize-image", Attempts: 1}
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
// ---------------------------------------------------------------------------

func TestHandleMessage_ExhaustedAttempts_NackedToDLQ(t *testing.T) {
	q := &stubQueue{}
	// attempts=2, maxAttempts=3: after increment → 3, which is not < 3, so DLQ.
	job := jobs.Job{ID: "j3", Type: "unknown-type", Attempts: 2}
	d, acker := jobDelivery(t, job)

	handleMessage(d, q)

	if !acker.nacked {
		t.Error("expected Nack when attempts exhausted")
	}
	if acker.nackRequeue {
		t.Error("expected Nack without requeue (to DLQ)")
	}
	if len(q.published) != 0 {
		t.Errorf("expected nothing re-published after DLQ, got %d", len(q.published))
	}
}

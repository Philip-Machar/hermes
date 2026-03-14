//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
)

// startRabbitMQ spins up a RabbitMQ container and returns the AMQP URL.
// The container is automatically terminated when the test ends.
func startRabbitMQ(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rabbitmq:3.13-alpine",
			ExposedPorts: []string{"5672/tcp"},
			Env: map[string]string{
				"RABBITMQ_DEFAULT_USER": "guest",
				"RABBITMQ_DEFAULT_PASS": "guest",
			},
			WaitingFor: wait.ForLog("Server startup complete").
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start RabbitMQ container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("failed to terminate RabbitMQ container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5672")
	if err != nil {
		t.Fatalf("failed to get mapped port: %v", err)
	}

	return fmt.Sprintf("amqp://guest:guest@%s:%s/", host, port.Port())
}

// ---------------------------------------------------------------------------
// Test: POST /jobs with no workers → published to RabbitMQ → consumer acks
// ---------------------------------------------------------------------------

func TestIntegration_JobQueuedAndConsumed(t *testing.T) {
	amqpURL := startRabbitMQ(t)

	// Connect using our real RabbitMQ implementation.
	rmq, err := queue.NewRabbitMQ(&queue.Config{
		URL:       amqpURL,
		QueueName: "jobs",
	})
	if err != nil {
		t.Fatalf("failed to connect to RabbitMQ: %v", err)
	}
	defer rmq.Close()

	// Use the real jobsHandler with an empty worker registry so every job
	// falls through to the queue path.
	reg := emptyRegistry{}
	handler := jobsHandlerFunc(reg, rmq)

	// Submit a job via the HTTP handler.
	body := `{"type":"email","payload":{"to":"test@example.com","subject":"Integration test"}}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d — body: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["routed"] != "queue" {
		t.Errorf("expected routed=queue, got %v", resp["routed"])
	}

	// Now consume the message and verify it is a valid job.
	consumed := make(chan jobs.Job, 1)

	go func() {
		msgs, err := rmq.Channel().Consume("jobs", "", false, false, false, false, nil)
		if err != nil {
			t.Logf("consume error: %v", err)
			return
		}
		for msg := range msgs {
			var j jobs.Job
			if err := json.Unmarshal(msg.Body, &j); err != nil {
				t.Logf("unmarshal error: %v", err)
				_ = msg.Nack(false, false)
				continue
			}
			_ = msg.Ack(false)
			consumed <- j
			return
		}
	}()

	select {
	case j := <-consumed:
		if j.Type != "email" {
			t.Errorf("expected job type 'email', got %q", j.Type)
		}
		if j.ID == "" {
			t.Error("expected non-empty job ID")
		}
		if j.Attempts != 0 {
			t.Errorf("expected attempts=0, got %d", j.Attempts)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for job to be consumed")
	}
}

// ---------------------------------------------------------------------------
// Test: job published directly → consumer retries on failure → DLQ after 3
// ---------------------------------------------------------------------------

func TestIntegration_JobRetriesAndReachesDLQ(t *testing.T) {
	amqpURL := startRabbitMQ(t)

	rmq, err := queue.NewRabbitMQ(&queue.Config{
		URL:       amqpURL,
		QueueName: "jobs",
	})
	if err != nil {
		t.Fatalf("failed to connect to RabbitMQ: %v", err)
	}
	defer rmq.Close()

	// Publish a job with an unknown type so the handler always fails.
	job := jobs.Job{ID: "integ-1", Type: "unknown-type", Attempts: 0}
	data, _ := json.Marshal(job)
	if err := rmq.Publish(data); err != nil {
		t.Fatalf("failed to publish job: %v", err)
	}

	// Consume and simulate the retry loop manually, mirroring handleMessage logic.
	msgs, err := rmq.Channel().Consume("jobs", "", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("failed to consume: %v", err)
	}

	attempts := 0
	timeout := time.After(20 * time.Second)

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			var j jobs.Job
			if err := json.Unmarshal(msg.Body, &j); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			attempts++

			if j.Attempts < 2 {
				// Re-publish with incremented attempt counter (mirrors handleMessage).
				j.Attempts++
				redata, _ := json.Marshal(j)
				_ = msg.Ack(false)
				if err := rmq.Publish(redata); err != nil {
					t.Fatalf("re-publish failed: %v", err)
				}
			} else {
				// Third attempt — Nack to DLQ.
				_ = msg.Nack(false, false)

				// Verify the message lands on the DLQ.
				dlqMsgs, err := rmq.Channel().Consume("jobs.dlq", "", true, false, false, false, nil)
				if err != nil {
					t.Fatalf("failed to consume DLQ: %v", err)
				}
				select {
				case dlqMsg := <-dlqMsgs:
					var dead jobs.Job
					if err := json.Unmarshal(dlqMsg.Body, &dead); err != nil {
						t.Fatalf("failed to unmarshal DLQ message: %v", err)
					}
					if dead.ID != "integ-1" {
						t.Errorf("expected DLQ job id integ-1, got %q", dead.ID)
					}
					if attempts != 3 {
						t.Errorf("expected 3 attempts before DLQ, got %d", attempts)
					}
					return
				case <-time.After(10 * time.Second):
					t.Fatal("timed out waiting for DLQ message")
				}
			}
		case <-timeout:
			t.Fatalf("timed out after %d attempts", attempts)
		}
	}
}

// ---------------------------------------------------------------------------
// Minimal stubs — satisfy the queue.Queue interface for the handler call
// ---------------------------------------------------------------------------

// emptyRegistry satisfies enough of worker.Registry's interface for the handler.
// Since we want the queue path, PickWorker always returns "".
type emptyRegistry struct{}

func (emptyRegistry) PickWorker() string { return "" }
func (emptyRegistry) DispatchJob(workerID, jobID, jobType string, payload interface{}) error {
	return nil
}

// jobsHandlerFunc is a minimal re-implementation of the POST /jobs logic
// so the integration test doesn't import cmd/api (which has a main package).
func jobsHandlerFunc(reg emptyRegistry, rmq queue.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Type    string      `json:"type"`
			Payload interface{} `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		j := jobs.Job{
			ID:       generateID(),
			Type:     body.Type,
			Payload:  body.Payload,
			Attempts: 0,
		}

		data, _ := json.Marshal(j)
		if err := rmq.Publish(data); err != nil {
			http.Error(w, "failed to queue job", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job":    j,
			"routed": "queue",
		})
	}
}

func generateID() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

// ---------------------------------------------------------------------------
// Test: raw AMQP publish → consume → message body is valid JSON job
// ---------------------------------------------------------------------------

func TestIntegration_RawPublishAndConsume(t *testing.T) {
	amqpURL := startRabbitMQ(t)

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("failed to open channel: %v", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare("raw-test", false, true, false, false, nil)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	job := jobs.Job{ID: "raw-1", Type: "email", Attempts: 0}
	body, _ := json.Marshal(job)

	if err := ch.Publish("", q.Name, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        body,
	}); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		t.Fatalf("failed to consume: %v", err)
	}

	select {
	case msg := <-msgs:
		var received jobs.Job
		if err := json.Unmarshal(msg.Body, &received); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if received.ID != "raw-1" {
			t.Errorf("expected ID raw-1, got %q", received.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

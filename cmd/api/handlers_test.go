package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jobqueue/internal/worker"
)

// ---------------------------------------------------------------------------
// Stub queue — satisfies queue.Queue without RabbitMQ
// ---------------------------------------------------------------------------

type stubQueue struct {
	published [][]byte
	failNext  bool
}

func (s *stubQueue) Publish(body []byte) error {
	if s.failNext {
		s.failNext = false
		return errors.New("publish failed")
	}
	s.published = append(s.published, body)
	return nil
}

func (s *stubQueue) Close() error { return nil }

// ---------------------------------------------------------------------------
// POST /jobs — wrong method
// ---------------------------------------------------------------------------

func TestJobsHandler_WrongMethod(t *testing.T) {
	reg := worker.NewRegistry()
	q := &stubQueue{}

	req := httptest.NewRequest(http.MethodGet, "/jobs", nil)
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /jobs — bad JSON body
// ---------------------------------------------------------------------------

func TestJobsHandler_InvalidBody(t *testing.T) {
	reg := worker.NewRegistry()
	q := &stubQueue{}

	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader("{bad json}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /jobs — no workers → routed to queue
// ---------------------------------------------------------------------------

func TestJobsHandler_NoWorkers_RoutedToQueue(t *testing.T) {
	reg := worker.NewRegistry() // empty registry
	q := &stubQueue{}

	body := `{"type":"email","payload":{"to":"a@b.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if resp["routed"] != "queue" {
		t.Errorf("expected routed=queue, got %v", resp["routed"])
	}
	if len(q.published) != 1 {
		t.Errorf("expected 1 published message, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// POST /jobs — worker alive → routed direct
// ---------------------------------------------------------------------------

func TestJobsHandler_WithWorker_RoutedDirect(t *testing.T) {
	// Spin up a fake worker dispatch server.
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer workerServer.Close()

	reg := worker.NewRegistry()
	// Strip scheme — registry prepends "http://" internally.
	reg.Register("w1", workerServer.Listener.Addr().String())

	q := &stubQueue{}

	body := `{"type":"email","payload":{"to":"a@b.com"}}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if resp["routed"] != "direct" {
		t.Errorf("expected routed=direct, got %v", resp["routed"])
	}
	if resp["worker_id"] != "w1" {
		t.Errorf("expected worker_id=w1, got %v", resp["worker_id"])
	}
	// Nothing should have been queued.
	if len(q.published) != 0 {
		t.Errorf("expected 0 published messages, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// POST /jobs — worker dispatch fails → falls back to queue
// ---------------------------------------------------------------------------

func TestJobsHandler_WorkerDispatchFails_FallsBackToQueue(t *testing.T) {
	// Fake worker that always returns 500.
	workerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer workerServer.Close()

	reg := worker.NewRegistry()
	reg.Register("w1", workerServer.Listener.Addr().String())

	q := &stubQueue{}

	body := `{"type":"email","payload":{}}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if resp["routed"] != "queue" {
		t.Errorf("expected routed=queue after dispatch failure, got %v", resp["routed"])
	}
	if len(q.published) != 1 {
		t.Errorf("expected 1 published message after fallback, got %d", len(q.published))
	}
}

// ---------------------------------------------------------------------------
// POST /jobs — response contains expected job fields
// ---------------------------------------------------------------------------

func TestJobsHandler_ResponseContainsJobFields(t *testing.T) {
	reg := worker.NewRegistry()
	q := &stubQueue{}

	body := `{"type":"resize-image","payload":{"url":"https://example.com/img.png"}}`
	req := httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	jobsHandler(reg, q)(rr, req)

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	jobField, ok := resp["job"].(map[string]interface{})
	if !ok {
		t.Fatal("expected job field in response")
	}
	if jobField["type"] != "resize-image" {
		t.Errorf("expected type resize-image, got %v", jobField["type"])
	}
	if jobField["id"] == "" || jobField["id"] == nil {
		t.Error("expected non-empty job id")
	}
}

// ---------------------------------------------------------------------------
// GET /workers — wrong method
// ---------------------------------------------------------------------------

func TestWorkersHandler_WrongMethod(t *testing.T) {
	reg := worker.NewRegistry()

	req := httptest.NewRequest(http.MethodPost, "/workers", nil)
	rr := httptest.NewRecorder()
	workersHandler(reg)(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /workers — empty registry
// ---------------------------------------------------------------------------

func TestWorkersHandler_EmptyRegistry(t *testing.T) {
	reg := worker.NewRegistry()

	req := httptest.NewRequest(http.MethodGet, "/workers", nil)
	rr := httptest.NewRecorder()
	workersHandler(reg)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["count"].(float64) != 0 {
		t.Errorf("expected count 0, got %v", resp["count"])
	}
}

// ---------------------------------------------------------------------------
// GET /workers — returns correct count and status
// ---------------------------------------------------------------------------

func TestWorkersHandler_ReturnsWorkersWithStatus(t *testing.T) {
	reg := worker.NewRegistry()
	reg.Register("w1", "localhost:9001")
	reg.Register("w2", "localhost:9002")
	reg.UpdateLoad("w2", 1) // mark w2 as busy

	req := httptest.NewRequest(http.MethodGet, "/workers", nil)
	rr := httptest.NewRecorder()
	workersHandler(reg)(rr, req)

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)

	if resp["count"].(float64) != 2 {
		t.Errorf("expected count 2, got %v", resp["count"])
	}

	workers := resp["workers"].([]interface{})
	statusByID := make(map[string]string)
	for _, w := range workers {
		wm := w.(map[string]interface{})
		statusByID[wm["worker_id"].(string)] = wm["status"].(string)
	}

	if statusByID["w1"] != "idle" {
		t.Errorf("expected w1 idle, got %s", statusByID["w1"])
	}
	if statusByID["w2"] != "busy" {
		t.Errorf("expected w2 busy, got %s", statusByID["w2"])
	}
}

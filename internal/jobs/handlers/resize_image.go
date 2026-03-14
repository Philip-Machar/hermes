package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"jobqueue/internal/jobs"
)

// ResizeImageHandler processes jobs of type "resize-image".
// Replace the log statement with a real image-processing call
// (e.g. imaging, vips, or an external service) when ready.
type ResizeImageHandler struct{}

type resizeImagePayload struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (h *ResizeImageHandler) Handle(ctx context.Context, job jobs.Job) error {
	raw, err := json.Marshal(job.Payload)
	if err != nil {
		return fmt.Errorf("resize-image handler: failed to encode payload: %w", err)
	}

	var p resizeImagePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("resize-image handler: invalid payload shape: %w", err)
	}

	if p.URL == "" {
		return fmt.Errorf("resize-image handler: missing required field 'url'")
	}

	// TODO: replace with real image-processing call.
	log.Printf("[resize-image] job=%s url=%s size=%dx%d — resized (simulated)", job.ID, p.URL, p.Width, p.Height)
	return nil
}

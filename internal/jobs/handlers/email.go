package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"jobqueue/internal/jobs"
)

// EmailHandler processes jobs of type "email".
// Replace the log statements with a real mail client (e.g. SendGrid, SES)
// when you are ready to send actual emails.
type EmailHandler struct{}

type emailPayload struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (h *EmailHandler) Handle(ctx context.Context, job jobs.Job) error {
	raw, err := json.Marshal(job.Payload)
	if err != nil {
		return fmt.Errorf("email handler: failed to decode payload: %w", err)
	}

	var p emailPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("email handler: invalid payload shape: %w", err)
	}

	if p.To == "" {
		return fmt.Errorf("email handler: missing required field 'to'")
	}

	// TODO: replace with real mail-client call.
	log.Printf("[email] job=%s to=%s subject=%q — sent (simulated)", job.ID, p.To, p.Subject)
	return nil
}

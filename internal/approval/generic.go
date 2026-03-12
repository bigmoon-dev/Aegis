package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// GenericWebhookNotifier sends a standard JSON POST to any webhook URL.
type GenericWebhookNotifier struct {
	webhookURL string
	client     *http.Client
}

func NewGenericWebhookNotifier(webhookURL string) *GenericWebhookNotifier {
	return &GenericWebhookNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GenericWebhookNotifier) Notify(req *PendingRequest, callbackBaseURL string, token string) error {
	if g.webhookURL == "" {
		log.Printf("[generic] webhook URL not configured, skipping notification")
		return nil
	}

	approveURL := fmt.Sprintf("%s/callback/approval?id=%s&action=approve&token=%s", callbackBaseURL, req.ID, token)
	rejectURL := fmt.Sprintf("%s/callback/approval?id=%s&action=reject&token=%s", callbackBaseURL, req.ID, token)

	payload := map[string]any{
		"event":       "approval_request",
		"id":          req.ID,
		"agent_id":    req.AgentID,
		"tool_name":   req.ToolName,
		"arguments":   req.Arguments,
		"created_at":  req.CreatedAt.Format(time.RFC3339),
		"approve_url": approveURL,
		"reject_url":  rejectURL,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal generic webhook payload: %w", err)
	}

	resp, err := g.client.Post(g.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send generic webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("generic webhook returned status %d", resp.StatusCode)
	}

	log.Printf("[generic] approval notification sent for request %s", req.ID)
	return nil
}

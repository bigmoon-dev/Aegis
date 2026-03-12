package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// FeishuNotifier sends interactive card messages to Feishu via webhook.
type FeishuNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewFeishuNotifier creates a notifier that sends interactive cards to Feishu.
func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
	return &FeishuNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends an approval request card to Feishu with approve/reject buttons.
func (f *FeishuNotifier) Notify(req *PendingRequest, callbackBaseURL string, token string) error {
	if f.webhookURL == "" {
		log.Printf("[feishu] webhook URL not configured, skipping notification")
		return nil
	}

	approveURL := fmt.Sprintf("%s/callback/approval?id=%s&action=approve&token=%s", callbackBaseURL, req.ID, token)
	rejectURL := fmt.Sprintf("%s/callback/approval?id=%s&action=reject&token=%s", callbackBaseURL, req.ID, token)

	// Truncate arguments for display
	args := req.Arguments
	if len(args) > 500 {
		args = args[:500] + "..."
	}

	card := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title": map[string]any{
					"tag":     "plain_text",
					"content": "🔐 Agent Approval Request",
				},
				"template": "orange",
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"fields": []any{
						map[string]any{
							"is_short": true,
							"text": map[string]any{
								"tag":     "lark_md",
								"content": fmt.Sprintf("**Agent:** %s", req.AgentID),
							},
						},
						map[string]any{
							"is_short": true,
							"text": map[string]any{
								"tag":     "lark_md",
								"content": fmt.Sprintf("**Tool:** %s", req.ToolName),
							},
						},
					},
				},
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**Arguments:**\n```\n%s\n```", args),
					},
				},
				map[string]any{
					"tag": "hr",
				},
				map[string]any{
					"tag": "action",
					"actions": []any{
						map[string]any{
							"tag": "button",
							"text": map[string]any{
								"tag":     "plain_text",
								"content": "Approve",
							},
							"type": "primary",
							"url":  approveURL,
						},
						map[string]any{
							"tag": "button",
							"text": map[string]any{
								"tag":     "plain_text",
								"content": "Reject",
							},
							"type": "danger",
							"url":  rejectURL,
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("marshal feishu card: %w", err)
	}

	resp, err := f.client.Post(f.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send feishu webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("feishu webhook returned status %d", resp.StatusCode)
	}

	log.Printf("[feishu] approval notification sent for request %s", req.ID)
	return nil
}

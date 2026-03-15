package setup

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/bigmoon-dev/aegis/internal/model"
	"github.com/charmbracelet/huh"
)

// BackendInput holds the user's backend configuration.
type BackendInput struct {
	Name string
	URL  string
}

// ToolPolicy holds per-tool policy choices.
type ToolPolicy struct {
	Name       string
	RateLimit  string // e.g. "5/1h", "unlimited"
	Approval   bool
	Queue      bool
	QueueDelay string // e.g. "30s-60s"
	Deny       bool
}

// AgentChoice holds the selected agent adapter and ID.
type AgentChoice struct {
	Adapter AgentAdapter
	AgentID string
}

// PromptBackend asks for backend name and URL, then discovers tools.
func PromptBackend() (*BackendInput, []model.ToolInfo, error) {
	var backendURL string

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("MCP server URL").
				Description("The HTTP URL of the MCP backend server").
				Placeholder("http://localhost:9200/mcp").
				Value(&backendURL).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("URL is required")
					}
					u, err := url.Parse(s)
					if err != nil || u.Scheme == "" || u.Host == "" {
						return fmt.Errorf("invalid URL format")
					}
					return nil
				}),
		),
	).Run()
	if err != nil {
		return nil, nil, err
	}

	// Infer default name from URL host/port
	defaultName := inferBackendName(backendURL)

	var backendName string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Backend name").
				Description("A short identifier for this backend").
				Placeholder(defaultName).
				Value(&backendName),
		),
	).Run()
	if err != nil {
		return nil, nil, err
	}
	if backendName == "" {
		backendName = defaultName
	}

	// Discover tools
	fmt.Printf("\n  Connecting to %s...", backendURL)
	tools, err := DiscoverTools(backendURL)
	if err != nil {
		fmt.Printf(" ✗\n")
		return nil, nil, fmt.Errorf("discovery failed: %w", err)
	}
	fmt.Printf(" ✓ Found %d tools\n\n", len(tools))

	// Display discovered tools
	for _, t := range tools {
		desc := t.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("    • %s — %s\n", t.Name, desc)
	}
	fmt.Println()

	return &BackendInput{Name: backendName, URL: backendURL}, tools, nil
}

// PromptToolPolicies asks the user to configure each tool's policy.
func PromptToolPolicies(tools []model.ToolInfo) ([]ToolPolicy, error) {
	policies := make([]ToolPolicy, 0, len(tools))

	for _, tool := range tools {
		def := inferDefaults(tool.Name)

		fmt.Printf("  %s", tool.Name)
		if tool.Description != "" {
			desc := tool.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			fmt.Printf(" (%s)", desc)
		}
		fmt.Println(":")

		var useDefaults bool
		err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Use defaults? [rate=%s, queue=%v, approval=%v, deny=%v]",
						def.RateLimit, def.Queue, def.Approval, def.Deny)).
					Value(&useDefaults).
					Affirmative("Yes").
					Negative("Customize"),
			),
		).Run()
		if err != nil {
			return nil, err
		}

		if useDefaults {
			policies = append(policies, def)
			fmt.Printf("    ✓ Using defaults\n\n")
			continue
		}

		// Custom configuration
		p := ToolPolicy{Name: tool.Name}

		if err := promptCustomPolicy(&p, def); err != nil {
			return nil, err
		}

		policies = append(policies, p)
		fmt.Println()
	}

	return policies, nil
}

func promptCustomPolicy(p *ToolPolicy, def ToolPolicy) error {
	rateOpts := []huh.Option[string]{
		huh.NewOption("unlimited", "unlimited"),
		huh.NewOption("1/24h", "1/24h"),
		huh.NewOption("5/1h", "5/1h"),
		huh.NewOption("10/1h", "10/1h"),
		huh.NewOption("20/1h", "20/1h"),
		huh.NewOption("50/1h", "50/1h"),
	}

	queueDelayOpts := []huh.Option[string]{
		huh.NewOption("No queue (bypass)", "none"),
		huh.NewOption("5s - 15s", "5s-15s"),
		huh.NewOption("30s - 60s", "30s-60s"),
		huh.NewOption("60s - 600s", "60s-600s"),
	}

	p.RateLimit = def.RateLimit
	queueDelay := "none"
	if def.Queue {
		queueDelay = def.QueueDelay
	}
	p.Approval = def.Approval
	p.Deny = def.Deny

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Rate limit").
				Options(rateOpts...).
				Value(&p.RateLimit),

			huh.NewSelect[string]().
				Title("Queue delay").
				Options(queueDelayOpts...).
				Value(&queueDelay),

			huh.NewConfirm().
				Title("Require human approval?").
				Value(&p.Approval).
				Affirmative("Yes").
				Negative("No"),

			huh.NewConfirm().
				Title("Deny this tool entirely?").
				Value(&p.Deny).
				Affirmative("Yes").
				Negative("No"),
		),
	).Run()
	if err != nil {
		return err
	}

	p.Queue = queueDelay != "none"
	p.QueueDelay = queueDelay

	return nil
}

// PromptAgent asks the user to select an agent framework and enter an agent ID.
func PromptAgent(backendName string) (*AgentChoice, error) {
	adapters := AllAdapters()

	// Build options with detection status
	opts := make([]huh.Option[int], 0, len(adapters))
	for i, a := range adapters {
		label := a.Name()
		if a.Detect() {
			label += "  ✓ Installed"
		} else if a.Name() != "Custom" {
			label += "  ✗ Not found"
		}
		opts = append(opts, huh.NewOption(label, i))
	}

	var selected int
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("Select agent framework").
				Options(opts...).
				Value(&selected),
		),
	).Run()
	if err != nil {
		return nil, err
	}

	adapter := adapters[selected]
	defaultID := strings.ToLower(strings.ReplaceAll(adapter.Name(), " ", "")) + "-" + backendName

	var agentID string
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Agent ID").
				Description("Unique identifier for this agent in Aegis config").
				Placeholder(defaultID).
				Value(&agentID),
		),
	).Run()
	if err != nil {
		return nil, err
	}
	if agentID == "" {
		agentID = defaultID
	}

	return &AgentChoice{Adapter: adapter, AgentID: agentID}, nil
}

// inferBackendName guesses a backend name from the URL.
func inferBackendName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "backend"
	}
	// Try to extract meaningful name from path
	path := strings.Trim(u.Path, "/")
	if path != "" && path != "mcp" {
		parts := strings.Split(path, "/")
		for _, p := range parts {
			if p != "mcp" && p != "" {
				return p
			}
		}
	}
	// Fall back to host
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" {
		return fmt.Sprintf("mcp-%s", u.Port())
	}
	return host
}

// inferDefaults returns smart default policies based on tool name patterns.
func inferDefaults(toolName string) ToolPolicy {
	name := strings.ToLower(toolName)

	// Read-only operations
	for _, kw := range []string{"list", "get", "check", "status"} {
		if strings.Contains(name, kw) {
			return ToolPolicy{
				Name: toolName, RateLimit: "unlimited",
				Queue: false, Approval: false, Deny: false,
			}
		}
	}

	// Search/query
	for _, kw := range []string{"search", "query"} {
		if strings.Contains(name, kw) {
			return ToolPolicy{
				Name: toolName, RateLimit: "20/1h",
				Queue: false, Approval: false, Deny: false,
			}
		}
	}

	// Download/fetch
	for _, kw := range []string{"download", "fetch"} {
		if strings.Contains(name, kw) {
			return ToolPolicy{
				Name: toolName, RateLimit: "5/1h",
				Queue: true, QueueDelay: "30s-60s", Approval: false, Deny: false,
			}
		}
	}

	// Write/publish/send
	for _, kw := range []string{"publish", "post", "write", "send"} {
		if strings.Contains(name, kw) {
			return ToolPolicy{
				Name: toolName, RateLimit: "1/24h",
				Queue: true, QueueDelay: "30s-60s", Approval: true, Deny: false,
			}
		}
	}

	// Dangerous operations
	for _, kw := range []string{"delete", "reset", "admin"} {
		if strings.Contains(name, kw) {
			return ToolPolicy{
				Name: toolName, RateLimit: "unlimited",
				Queue: false, Approval: false, Deny: true,
			}
		}
	}

	// Default
	return ToolPolicy{
		Name: toolName, RateLimit: "10/1h",
		Queue: true, QueueDelay: "5s-15s", Approval: false, Deny: false,
	}
}

// ApprovalNotificationInput holds the user's approval notification configuration.
type ApprovalNotificationInput struct {
	FeishuWebhookURL  string
	GenericWebhookURL string
	CallbackBaseURL   string
}

// PromptApprovalNotification asks how approval requests should be delivered.
func PromptApprovalNotification() (ApprovalNotificationInput, error) {
	var result ApprovalNotificationInput

	var channel string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How should approval requests be delivered?").
				Options(
					huh.NewOption("Feishu / Lark", "feishu"),
					huh.NewOption("Slack / Discord / Generic Webhook", "generic"),
					huh.NewOption("Both (Feishu + Generic)", "both"),
					huh.NewOption("Skip (approve via Management API only)", "skip"),
				).
				Value(&channel),
		),
	).Run()
	if err != nil {
		return result, err
	}

	if channel == "skip" {
		return result, nil
	}

	needFeishu := channel == "feishu" || channel == "both"
	needGeneric := channel == "generic" || channel == "both"

	var fields []huh.Field

	if needFeishu {
		fields = append(fields,
			huh.NewInput().
				Title("Feishu Webhook URL").
				Description("群设置 → 群机器人 → 添加自定义机器人 → 复制 Webhook 地址").
				Placeholder("https://open.feishu.cn/open-apis/bot/v2/hook/...").
				Value(&result.FeishuWebhookURL).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("webhook URL is required")
					}
					u, err := url.Parse(s)
					if err != nil || u.Scheme == "" || u.Host == "" {
						return fmt.Errorf("invalid URL format")
					}
					return nil
				}),
		)
	}

	if needGeneric {
		fields = append(fields,
			huh.NewInput().
				Title("Generic Webhook URL").
				Description("POST JSON payload to this URL when approval is needed").
				Placeholder("https://hooks.slack.com/services/...").
				Value(&result.GenericWebhookURL).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("webhook URL is required")
					}
					u, err := url.Parse(s)
					if err != nil || u.Scheme == "" || u.Host == "" {
						return fmt.Errorf("invalid URL format")
					}
					return nil
				}),
		)
	}

	defaultCallback := fmt.Sprintf("http://%s:18070", detectLocalIP())
	result.CallbackBaseURL = defaultCallback

	fields = append(fields,
		huh.NewInput().
			Title("Callback Base URL").
			Description("Approval buttons redirect here — ensure your device can reach it").
			Placeholder(defaultCallback).
			Value(&result.CallbackBaseURL),
	)

	err = huh.NewForm(huh.NewGroup(fields...)).Run()
	if err != nil {
		return result, err
	}

	if result.CallbackBaseURL == "" {
		result.CallbackBaseURL = defaultCallback
	}

	return result, nil
}

// detectLocalIP returns the first non-loopback IPv4 address, or "127.0.0.1" as fallback.
func detectLocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}

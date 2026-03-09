package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/bigmoon-dev/aegis/internal/config"
	"github.com/bigmoon-dev/aegis/internal/model"
)

// EnhanceToolsList filters and enhances tools/list responses per agent.
func EnhanceToolsList(cfg *config.Config, agentID, backendID string, result *model.ToolsListResult) *model.ToolsListResult {
	agentCfg, ok := cfg.Agents[agentID]
	if !ok {
		return result
	}
	backendCfg, ok := agentCfg.Backends[backendID]
	if !ok {
		return result
	}

	// Get global rate limits for this backend
	var globalLimits map[string]config.RateLimitConfig
	if qCfg, ok := cfg.Queue[backendID]; ok {
		globalLimits = qCfg.GlobalRateLimits
	}

	filtered := make([]model.ToolInfo, 0, len(result.Tools))
	for _, tool := range result.Tools {
		// Remove denied tools
		if slices.Contains(backendCfg.ToolDenylist, tool.Name) {
			log.Printf("[toolslist] hiding tool %q from agent %q (denylist)", tool.Name, agentID)
			continue
		}

		// Inject constraint annotations into description
		var annotations []string

		if rl, ok := backendCfg.RateLimits[tool.Name]; ok {
			annotations = append(annotations, fmt.Sprintf("Rate:%d/%s", rl.MaxCount, formatDuration(rl.Window)))
		}

		if gl, ok := globalLimits[tool.Name]; ok {
			annotations = append(annotations, fmt.Sprintf("GlobalRate:%d/%s", gl.MaxCount, formatDuration(gl.Window)))
		}

		if slices.Contains(backendCfg.ApprovalRequired, tool.Name) {
			annotations = append(annotations, "ApprovalRequired")
		}

		if len(annotations) > 0 {
			prefix := "[" + strings.Join(annotations, "|") + "] "
			tool.Description = prefix + tool.Description
		}

		filtered = append(filtered, tool)
	}

	return &model.ToolsListResult{Tools: filtered}
}

// ParseToolsListResult unmarshals the tools/list result from a JSON-RPC response.
func ParseToolsListResult(resp *model.Response) (*model.ToolsListResult, error) {
	if resp.Error != nil {
		return nil, fmt.Errorf("backend error: %s", resp.Error.Message)
	}
	var result model.ToolsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools/list result: %w", err)
	}
	return &result, nil
}

func formatDuration(d interface{ Hours() float64 }) string {
	hours := d.Hours()
	if hours >= 24 {
		return fmt.Sprintf("%.0fd", hours/24)
	}
	if hours >= 1 {
		return fmt.Sprintf("%.0fh", hours)
	}
	minutes := hours * 60
	return fmt.Sprintf("%.0fm", minutes)
}

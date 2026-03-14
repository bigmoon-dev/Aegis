package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bigmoon-dev/aegis/internal/setup"
	"github.com/charmbracelet/huh"
)

func runSetup() error {
	fmt.Println()
	fmt.Println("  Aegis MCP — Interactive Setup")
	fmt.Println("  ─────────────────────────────")
	fmt.Println()

	// Step 1: Backend
	backend, tools, err := setup.PromptBackend()
	if err != nil {
		return fmt.Errorf("backend config: %w", err)
	}

	if len(tools) == 0 {
		fmt.Println("  ⚠ No tools found on this backend. Nothing to configure.")
		return nil
	}

	// Step 2: Tool policies
	fmt.Println("  ── Tool policies ──────────────────────────")
	fmt.Println()
	policies, err := setup.PromptToolPolicies(tools)
	if err != nil {
		return fmt.Errorf("tool policies: %w", err)
	}

	// Step 3: Agent selection
	fmt.Println("  ── Agent ────────────────────────────────")
	fmt.Println()
	agent, err := setup.PromptAgent(backend.Name)
	if err != nil {
		return fmt.Errorf("agent config: %w", err)
	}

	// Determine output path
	outputPath := "config/aegis.yaml"
	if _, err := os.Stat(outputPath); err == nil {
		// File exists, ask what to do
		var overwrite bool
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("%s already exists. Overwrite?", outputPath)).
					Value(&overwrite).
					Affirmative("Overwrite").
					Negative("Choose new path"),
			),
		).Run(); err != nil {
			return err
		}
		if !overwrite {
			var newPath string
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Config output path").
						Placeholder("config/aegis-new.yaml").
						Value(&newPath),
				),
			).Run(); err != nil {
				return err
			}
			if newPath != "" {
				outputPath = newPath
			}
		}
	}

	// Build Aegis URL for agent config
	aegisURL := fmt.Sprintf("http://localhost:18070/agents/%s/mcp", agent.AgentID)

	// Summary
	fmt.Println()
	fmt.Println("  ── Summary ──────────────────────────────")
	fmt.Println()
	fmt.Printf("  Config:   %s\n", outputPath)
	fmt.Printf("  Backend:  %s → %s (%d tools)\n", backend.Name, backend.URL, len(tools))
	fmt.Printf("  Agent:    %s (%s)\n", agent.AgentID, agent.Adapter.Name())

	if agent.Adapter.ConfigPath() != "" {
		fmt.Printf("  Inject:   %s\n", agent.Adapter.ConfigPath())
		fmt.Printf("            + \"%s\" → %s\n", backend.Name, aegisURL)
	}

	// Policy summary
	fmt.Println()
	for _, p := range policies {
		status := fmt.Sprintf("rate=%s", p.RateLimit)
		if p.Queue {
			status += fmt.Sprintf(", queue=%s", p.QueueDelay)
		}
		if p.Approval {
			status += ", approval"
		}
		if p.Deny {
			status = "DENIED"
		}
		fmt.Printf("    %-30s %s\n", p.Name, status)
	}
	fmt.Println()

	var confirm bool
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Write configuration?").
				Value(&confirm).
				Affirmative("Yes, write files").
				Negative("Cancel"),
		),
	).Run(); err != nil {
		return err
	}
	if !confirm {
		fmt.Println("  Cancelled.")
		return nil
	}

	// Write Aegis config
	if err := setup.GenerateConfig(*backend, policies, *agent, outputPath); err != nil {
		return fmt.Errorf("generate config: %w", err)
	}
	fmt.Printf("  ✓ Written: %s\n", outputPath)

	// Inject agent config
	if agent.Adapter.ConfigPath() != "" {
		err := agent.Adapter.Inject(backend.Name, aegisURL)
		if err != nil {
			errMsg := err.Error()
			if strings.HasPrefix(errMsg, "SKIP:") {
				fmt.Printf("  ⊘ %s\n", errMsg)
			} else if strings.HasPrefix(errMsg, "CONFLICT:") {
				fmt.Printf("  ⚠ %s\n", errMsg)
				var overwrite bool
				if err := huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Overwrite existing agent config entry?").
							Value(&overwrite).
							Affirmative("Overwrite").
							Negative("Skip"),
					),
				).Run(); err != nil {
					return err
				}
				if overwrite {
					if err := forceInject(agent, backend.Name, aegisURL); err != nil {
						fmt.Printf("  ✗ Failed to update agent config: %v\n", err)
					} else {
						fmt.Printf("  ✓ Updated: %s\n", agent.Adapter.ConfigPath())
					}
				}
			} else {
				fmt.Printf("  ✗ Agent config: %v\n", err)
			}
		} else {
			fmt.Printf("  ✓ Backup:  %s.bak\n", agent.Adapter.ConfigPath())
			fmt.Printf("  ✓ Updated: %s\n", agent.Adapter.ConfigPath())
			fmt.Printf("  ✓ Verified: JSON valid\n")
		}
	}

	// Post-setup hints
	fmt.Println()
	fmt.Println("  ⚠ Next steps:")
	fmt.Printf("    1. Start Aegis:   aegis %s\n", outputPath)
	fmt.Printf("    2. %s\n", agent.Adapter.PostSetupHint())
	fmt.Println()
	fmt.Println("  Done!")
	fmt.Println()

	return nil
}

// forceInject removes the existing entry from agent config JSON and re-injects.
func forceInject(agent *setup.AgentChoice, serverName, aegisURL string) error {
	configPath := agent.Adapter.ConfigPath()

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}

	if servers, ok := parsed["mcpServers"].(map[string]interface{}); ok {
		delete(servers, serverName)
	}

	out, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.WriteFile(configPath, out, 0644); err != nil {
		return err
	}

	return agent.Adapter.Inject(serverName, aegisURL)
}

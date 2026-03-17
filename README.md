# Aegis MCP

[![CI](https://github.com/bigmoon-dev/Aegis/actions/workflows/ci.yml/badge.svg)](https://github.com/bigmoon-dev/Aegis/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-90.7%25-brightgreen)](docs/testing.md)
[![Go Report Card](https://goreportcard.com/badge/github.com/bigmoon-dev/aegis)](https://goreportcard.com/report/github.com/bigmoon-dev/aegis)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**The MCP governance proxy that turns soft rules into hard constraints.**

English | [中文](README_CN.md)

Aegis MCP sits between your AI agents and MCP tool servers as a protocol-level proxy, enforcing constraints that agents cannot bypass — rate limiting, access control, human approval workflows, serialized execution queues, and full audit logging. Zero code changes required for any MCP-compatible agent.

```
         AI Agents
    ┌────────┬────────┐
  Agent    Agent    Agent
    A        B        C
    │        │        │
    └────┬───┴───┬────┘
         ▼       ▼
┌────────────────────────────────┐
│       Aegis MCP (:18070)       │
│                                │
│  Pipeline:                     │
│  ① ACL → ② Rate Limit         │
│  → ③ Human Approval            │
│  → ④ FIFO Queue → ⑤ Forward   │
│  → ⑥ Audit Log                │
└───────────────┬────────────────┘
                ▼
┌────────────────────────────────┐
│       MCP Tool Server          │
│       (e.g. social media,      │
│        database, APIs...)      │
└────────────────────────────────┘
```

<p align="center">
  <img src="docs/demo-preview.svg" alt="Aegis MCP Demo — ACL filtering, rate limiting, human approval, audit logging" width="820">
</p>

## Why an MCP Proxy?

AI agents are powerful but unreliable rule followers. Prompt-based "soft rules" like "don't post more than once per day" are routinely violated. When agents operate on real accounts — social media, e-commerce, customer service — a single burst of unchecked actions can trigger platform bans, compliance violations, or worse.

Aegis MCP converts soft rules into **programmatic hard constraints** at the MCP protocol level. The agent literally cannot exceed its rate limit or skip the approval step, regardless of what the LLM decides to do.

Unlike SDK-based approaches that require code changes in each agent, Aegis MCP works as a **transparent proxy** — point your agent to Aegis MCP instead of the backend, and governance is enforced automatically. Works with any MCP-compatible agent: Claude Code, OpenClaw, custom agents, and more.

## Features

- **Access Control (ACL)** — Per-agent, per-backend, per-tool allow/deny rules. Denied tools are invisible to the agent (removed from `tools/list` responses).

- **Two-Level Rate Limiting** — Per-agent sliding window limits *and* global cross-agent limits. All agents sharing one account? Global limits prevent cumulative overuse.

- **Human Approval Workflows** — Destructive operations (publishing, deleting) require human approval via webhook notifications with HMAC-signed callback URLs. Supports Feishu/Lark, generic webhooks (Slack, Discord, custom systems), or both simultaneously. Configurable timeout with auto-reject.

- **FIFO Execution Queue** — Per-backend serialized execution with randomized delays (1-10 min configurable) between operations. Mimics human interaction patterns. Bypass list for read-only tools.

- **Audit Logging** — Every tool call is recorded in SQLite: agent, tool, arguments, ACL/rate-limit/approval verdicts, queue position, execution duration, result. Auto-purge with configurable retention.

- **Tool Description Enhancement** — Constraints are injected into tool descriptions so the agent sees `[Rate:1/1d|ApprovalRequired] Publish post` instead of just `Publish post`. The agent is aware of its limits before deciding what to do.

- **Hot Reload** — Update config without restart via `POST /api/v1/config/reload`.

- **Single Binary** — Written in Go with only 3 dependencies (SQLite, YAML, UUID). Runs on a Raspberry Pi.

## Installation

### npm (recommended for MCP users)

```bash
npx aegis-mcp-proxy config/aegis.yaml
# Or install globally:
npm install -g aegis-mcp-proxy
aegis-mcp-proxy config/aegis.yaml
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/bigmoon-dev/Aegis/releases):

```bash
tar xzf aegis_v*.tar.gz
./aegis config/aegis.yaml
```

### Docker

```bash
docker run --rm -v $(pwd)/config:/config ghcr.io/bigmoon-dev/aegis /config/aegis.yaml
```

### go install

Requires Go 1.24+ and CGO (gcc):

```bash
CGO_ENABLED=1 go install github.com/bigmoon-dev/aegis/cmd/aegis@latest
aegis config/aegis.yaml
```

### Build from source

```bash
git clone https://github.com/bigmoon-dev/Aegis.git
cd Aegis
make build
./aegis config/aegis.yaml
```

## Quickstart

Experience Aegis in 2 minutes:

```bash
npx aegis-mcp-proxy demo
```

Follow the on-screen curl examples to see ACL filtering, rate limiting, human approval, and audit logging in action.

Ready to connect your own MCP server? Pick your agent:

| Guide | Agent | Time |
|-------|-------|------|
| [Claude Code Guide](docs/guide-claude-code.md) | Claude Code (Anthropic) | 5 min |
| [OpenClaw Guide](docs/guide-openclaw.md) | OpenClaw | 10 min |
| [Generic Agent Guide](docs/guide-generic.md) | Any MCP-compatible agent | 5 min |

> **中文版**: [Claude Code](docs/guide-claude-code_cn.md) · [OpenClaw](docs/guide-openclaw_cn.md) · [通用 Agent](docs/guide-generic_cn.md)

### Interactive Setup (Recommended for New Users)

The quickest way to get a working configuration:

```bash
./aegis setup
# or: npx aegis-mcp-proxy setup
```

The wizard walks you through:

1. **Backend URL** — enter your MCP server address, Aegis connects and discovers available tools
2. **Per-tool policies** — smart defaults based on tool names (read-only tools get unlimited access, write/publish tools get rate limits + approval, dangerous tools get denied)
3. **Agent config** — auto-detects installed agents and injects the Aegis proxy URL into their config
4. **Approval notifications** — configure Feishu/Lark or generic webhook URLs for approval request delivery (only when tools require approval). Auto-detects your local IP for callback URLs

Supported agents:

| Agent | Config File | Detection |
|-------|------------|-----------|
| OpenClaw | `~/.openclaw/workspace/config/mcporter.json` | Auto-detected |
| Claude Code | `~/.claude/mcp_servers.json` | Auto-detected |
| Custom | — | Manual setup |

The wizard creates a `.bak` backup before modifying any agent config and validates JSON after writing. If an entry already exists for the same backend, it prompts for conflict resolution.

### Try the Interactive Demo

Experience all Aegis features in 5 minutes — no backend setup required. Just needs Node.js:

```bash
./aegis demo
# or: npx aegis-mcp-proxy demo
```

This starts a mock MCP server + Aegis proxy with a pre-configured policy. The terminal prints curl commands to try each feature:

| Step | What happens |
|------|-------------|
| `tools/list` | `admin_reset` is hidden by ACL — only 4 tools visible |
| `echo` | Passes through with no restrictions |
| `get_weather` ×4 | First 3 succeed, 4th is rate-limited (`-32002`) |
| `publish_post` | Blocks until you approve via management API |
| `list_posts` | Bypasses FIFO queue, returns immediately |
| `audit/logs` | Shows full audit trail of all operations |

### Cross-compile for Raspberry Pi

```bash
# Requires a cross-compiler (e.g. aarch64-linux-gnu-gcc) for CGO/SQLite
CC=aarch64-linux-gnu-gcc make cross-rpi
scp aegis user@your-server:~/aegis/

# Or build directly on the server:
ssh user@your-server 'cd ~/aegis && make build'
```

## Configuration

See the **[Policy Configuration Guide](docs/policy-guide.md)** for detailed documentation on writing policy rules, including field explanations, time formats, policy interactions, and common scenarios.

Basic example:

```yaml
server:
  listen: ":18070"
  read_timeout: 300s
  write_timeout: 300s
  # api_token: "your-secret-token"   # Protect /api/v1/ endpoints

backends:
  my-tools:
    url: "http://localhost:8080/mcp"    # Your MCP tool server
    health_url: "http://localhost:8080/health"
    timeout: 120s

queue:
  my-tools:
    enabled: true
    delay_min: 60s                      # Min delay between operations
    delay_max: 600s                     # Max delay between operations
    max_pending: 50
    bypass_tools:                       # Skip queue (still rate-limited)
      - "health_check"
    global_rate_limits:                 # Across ALL agents
      risky_operation: { window: 1h, max_count: 10 }

agents:
  production-agent:
    display_name: "Production Agent"
    auth_token: "your-secret-token-here"  # Optional: require Bearer token auth
    backends:
      my-tools:
        allowed: true
        tool_denylist: ["dangerous_tool"]
        rate_limits:
          publish: { window: 24h, max_count: 1 }
        approval_required:
          - "publish"
          - "delete"

  dev-agent:
    display_name: "Dev Agent"
    backends:
      my-tools:
        allowed: true
        tool_denylist: ["publish", "delete", "dangerous_tool"]

approval:
  feishu:
    webhook_url: ""                     # Your Feishu/Lark webhook URL
  generic:
    webhook_url: ""                     # Any webhook URL (Slack, Discord, custom, etc.)
  timeout: 600s
  callback_base_url: "http://your-server:18070"

audit:
  db_path: "./data/audit.db"
  retention_days: 90
```

## Pipeline

Every `tools/call` request passes through:

| Stage | Purpose | On Reject |
|-------|---------|-----------|
| **ACL** | Agent allowed to call this tool? | JSON-RPC `-32001` |
| **Rate Limiter** | Global + per-agent sliding window check | JSON-RPC `-32002` |
| **Approval Gate** | Human approval via webhook notification | JSON-RPC `-32004` (timeout) |
| **FIFO Queue** | Serialized execution with random delays | JSON-RPC `-32003` (full) |
| **Forwarder** | Proxy to backend MCP server | Backend error |
| **Audit Logger** | Record everything to SQLite | — |

Only successful calls count against rate limits (failed calls don't consume quota).

## Management API

When `server.api_token` is set, all `/api/v1/` endpoints require `Authorization: Bearer <token>`. When not set, the API is open (suitable for localhost-only deployments).

```
GET  /health                           # Service + backend health
GET  /api/v1/queue/status              # Pending items per backend
GET  /api/v1/agents                    # Agent list with permissions
GET  /api/v1/agents/{id}/rate-limits   # Current usage vs limits (agent + global scope)
GET  /api/v1/approvals/pending         # Awaiting human decision
POST /api/v1/approvals/{id}/approve    # Approve via API
POST /api/v1/approvals/{id}/reject     # Reject via API
GET  /api/v1/audit/logs                # Query audit log (?limit=50&offset=0)
POST /api/v1/config/reload             # Hot reload configuration
```

## How It Works

1. Agent sends MCP requests to `http://aegis:18070/agents/{agent-id}/mcp`
2. Aegis MCP identifies the agent from the URL path
3. `tools/list` → fetches from backend, filters denied tools, injects constraint annotations
4. `tools/call` → runs through the full pipeline (ACL → Rate Limit → Approval → Queue → Forward → Audit)
5. `initialize`, `ping`, etc. → passed through transparently
6. Agent sees only what it's allowed to see, can only do what it's allowed to do

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| MCP Proxy (not SDK/framework integration) | Dynamic tool forwarding; zero code changes; works with any MCP-compatible agent |
| SQLite (not Redis) | Minimal dependencies; persistent audit trail; low resource usage |
| Per-backend queue | All agents sharing one account must serialize globally |
| HMAC-signed approval callbacks | Prevent unauthorized approval via URL guessing |
| UTC everywhere | Avoid DST issues in rate limit window calculations |
| Global + per-agent rate limits | Multi-agent cumulative frequency control for shared accounts |

## Generic Webhook Payload

When `approval.generic.webhook_url` is configured, Aegis sends a POST request with `Content-Type: application/json`:

```json
{
  "event": "approval_request",
  "id": "request-uuid",
  "agent_id": "production-agent",
  "tool_name": "publish",
  "arguments": "{...}",
  "created_at": "2026-03-12T10:00:00Z",
  "approve_url": "http://aegis:18070/callback/approval?id=xxx&action=approve&token=xxx",
  "reject_url": "http://aegis:18070/callback/approval?id=xxx&action=reject&token=xxx"
}
```

To approve or reject, make a GET request to the corresponding URL. Both Feishu and generic webhook can be configured simultaneously — both will be notified.

## Development

### Running Tests

```bash
# All tests
CGO_ENABLED=1 go test ./... -count=1

# With race detector (recommended)
CGO_ENABLED=1 go test -race ./... -count=1

# With coverage
CGO_ENABLED=1 go test -cover ./internal/...
```

Tests use temporary SQLite databases and in-memory configs — no external dependencies required. CI runs `go test -race` on every push and pull request.

## Security

- **Agent endpoint authentication**: Set `auth_token` per agent in your config to require `Authorization: Bearer <token>` on `/agents/{agentID}/mcp`. Tokens must be at least 16 characters. Agents without `auth_token` remain open (suitable for local/demo use). **Always set this in production.**
- **Management API authentication**: Set `server.api_token` in your config to require `Authorization: Bearer <token>` on all `/api/v1/` endpoints. Without this, anyone who can reach the port can approve operations, view audit logs, and reload config. **Always set this in production.**
- **Approval callbacks**: Protected by per-request HMAC-SHA256 tokens (generated from a random 32-byte key at startup). Tokens are validated with constant-time comparison.
- **Request size limits**: Incoming requests are capped at 1 MB, backend responses at 10 MB.
- **TLS**: Aegis listens on plain HTTP. For production, place it behind a reverse proxy (nginx, Caddy) or bind to localhost only.
- **HMAC key lifecycle**: The HMAC signing key is generated in-memory at startup. Restarting Aegis invalidates all pending approval tokens.

## Requirements

- Go 1.24+ (CGO enabled for SQLite)
- An MCP-compatible tool server as backend

## License

[Apache 2.0](LICENSE)

## Acknowledgements

Built for governing AI agents operating on real platforms, born from hard-won lessons in social media automation risk control.

<!-- GitHub Topics: mcp, mcp-proxy, mcp-gateway, mcp-server, agent-governance, ai-agent, rate-limiting, access-control, audit-logging -->

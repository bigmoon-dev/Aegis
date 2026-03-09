# Aegis

**A governance proxy for AI agents using the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/).**

English | [中文](README_CN.md)

Aegis sits between your AI agents and MCP tool servers, enforcing hard constraints that agents cannot bypass — rate limiting, access control, human approval workflows, serialized execution queues, and full audit logging.

```
         AI Agents
    ┌────────┬────────┐
  Agent    Agent    Agent
    A        B        C
    │        │        │
    └────┬───┴───┬────┘
         ▼       ▼
┌──────────────────────────────┐
│        Aegis (:18070)        │
│                              │
│  Pipeline:                   │
│  ① ACL → ② Rate Limit       │
│  → ③ Human Approval          │
│  → ④ FIFO Queue → ⑤ Forward │
│  → ⑥ Audit Log              │
└──────────────┬───────────────┘
               ▼
┌──────────────────────────────┐
│     MCP Tool Server          │
│     (e.g. social media,      │
│      database, APIs...)      │
└──────────────────────────────┘
```

## Why

AI agents are powerful but unreliable rule followers. Prompt-based "soft rules" like "don't post more than once per day" are routinely violated. When agents operate on real accounts — social media, e-commerce, customer service — a single burst of unchecked actions can trigger platform bans, compliance violations, or worse.

Aegis converts soft rules into **programmatic hard constraints** at the protocol level. The agent literally cannot exceed its rate limit or skip the approval step, regardless of what the LLM decides to do.

## Features

- **Access Control (ACL)** — Per-agent, per-backend, per-tool allow/deny rules. Denied tools are invisible to the agent (removed from `tools/list` responses).

- **Two-Level Rate Limiting** — Per-agent sliding window limits *and* global cross-agent limits. All agents sharing one account? Global limits prevent cumulative overuse.

- **Human Approval Workflows** — Destructive operations (publishing, deleting) require human approval via Feishu/Lark webhook notifications with HMAC-signed callback URLs. Configurable timeout with auto-reject.

- **FIFO Execution Queue** — Per-backend serialized execution with randomized delays (1-10 min configurable) between operations. Mimics human interaction patterns. Bypass list for read-only tools.

- **Audit Logging** — Every tool call is recorded in SQLite: agent, tool, arguments, ACL/rate-limit/approval verdicts, queue position, execution duration, result. Auto-purge with configurable retention.

- **Tool Description Enhancement** — Constraints are injected into tool descriptions so the agent sees `[Rate:1/1d|ApprovalRequired] Publish post` instead of just `Publish post`. The agent is aware of its limits before deciding what to do.

- **Hot Reload** — Update config without restart via `POST /api/v1/config/reload`.

- **Single Binary** — Written in Go with only 3 dependencies (SQLite, YAML, UUID). Runs on a Raspberry Pi.

## Quick Start

```bash
# Build
make build

# Initialize config from example
make init-config
# Edit config/aegis.yaml with your settings

# Run
./aegis config/aegis.yaml
```

Point your MCP client to `http://localhost:18070/agents/{agent-id}/mcp` instead of the backend directly.

### Cross-compile for Raspberry Pi

```bash
# Requires a cross-compiler (e.g. aarch64-linux-gnu-gcc) for CGO/SQLite
CC=aarch64-linux-gnu-gcc make cross-rpi
scp aegis user@your-server:~/aegis/

# Or build directly on the server:
ssh user@your-server 'cd ~/aegis && make build'
```

## Configuration

```yaml
server:
  listen: ":18070"
  read_timeout: 300s
  write_timeout: 300s

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
2. Aegis identifies the agent from the URL path
3. `tools/list` → fetches from backend, filters denied tools, injects constraint annotations
4. `tools/call` → runs through the full pipeline (ACL → Rate Limit → Approval → Queue → Forward → Audit)
5. `initialize`, `ping`, etc. → passed through transparently
6. Agent sees only what it's allowed to see, can only do what it's allowed to do

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| MCP Proxy (not SDK Server) | Dynamic tool forwarding; no upstream source modification needed |
| SQLite (not Redis) | Minimal dependencies; persistent audit trail; low resource usage |
| Per-backend queue | All agents sharing one account must serialize globally |
| HMAC-signed approval callbacks | Prevent unauthorized approval via URL guessing |
| UTC everywhere | Avoid DST issues in rate limit window calculations |
| Global + per-agent rate limits | Multi-agent cumulative frequency control for shared accounts |

## Requirements

- Go 1.22+ (CGO enabled for SQLite)
- An MCP-compatible tool server as backend

## License

[AGPL-3.0](LICENSE)

## Acknowledgements

Built for governing AI agents operating on real platforms, born from hard-won lessons in social media automation risk control.

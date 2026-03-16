# Getting Started with Aegis MCP — Generic Agent

This guide walks you through protecting your MCP tool server with Aegis when using any MCP-compatible agent.

**Time**: ~5 minutes

## Prerequisites

- Node.js >= 18 (for `npx` commands) or [download the binary](https://github.com/bigmoon-dev/Aegis/releases)
- A running MCP tool server you want to protect
- An MCP-compatible agent that can be configured to point to a custom MCP server URL

## Step 1: Experience the Demo

Before connecting your own server, try the interactive demo to see all Aegis features in action.

### Install & Run

```bash
npx aegis-mcp-proxy demo
```

This starts a mock MCP server and Aegis proxy with a pre-configured policy. The terminal prints curl commands you can try.

### What You'll See

| Command | What Happens |
|---------|-------------|
| `tools/list` | 5 mock tools discovered, `admin_reset` hidden by ACL — only 4 visible |
| `echo` | Passes through with no restrictions |
| `get_weather` x4 | First 3 succeed, 4th blocked by rate limit (3/min) |
| `publish_post` | Blocks until you approve via the management API |
| `list_posts` | Bypasses FIFO queue, returns immediately |
| `audit/logs` | Shows full audit trail of all operations |

### Verify

```bash
# Health check
curl localhost:18070/health

# View audit logs
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

When you're done exploring, press `Ctrl+C` to stop the demo.

## Step 2: Protect Your MCP Server

### 2.1 Run the Setup Wizard

```bash
aegis setup
# or: npx aegis-mcp-proxy setup
```

Follow the prompts:

1. **Backend URL** — Enter your MCP server address (e.g., `http://localhost:9200/mcp`). Aegis connects and discovers available tools.
2. **Per-tool policies** — Review smart defaults based on tool names. Read-only tools get unlimited access; write/publish tools get rate limits + approval; dangerous tools are denied.
3. **Agent type** — Select **Custom**. Enter your agent ID (e.g., `my-agent`). This ID will be part of the Aegis proxy URL.
4. **Approval notifications** (optional) — Configure a Feishu/Lark or generic webhook URL for approval request delivery. The wizard auto-detects your local IP for callback URLs.

### 2.2 Start Aegis

```bash
./aegis config/aegis.yaml
# or: npx aegis-mcp-proxy config/aegis.yaml
```

### 2.3 Update Your Agent Config

Change your agent's MCP server URL from the direct backend address to the Aegis proxy URL:

```
# Before (direct to backend)
http://localhost:9200/mcp

# After (via Aegis)
http://localhost:18070/agents/my-agent/mcp
```

Replace `my-agent` with the agent ID you chose during setup, and `9200` with your actual backend port.

### 2.4 Verify

```bash
# Check Aegis health
curl localhost:18070/health

# View audit log — you should see tool calls after your agent interacts with the backend
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

### What Happened

The setup wizard performed these changes:

1. **Created** `config/aegis.yaml` — Aegis policy config with your backend, agent, and tool policies.

2. **Printed** the proxy URL for manual configuration:
   ```
   http://localhost:18070/agents/my-agent/mcp
   ```
   Point your agent to this URL instead of the backend directly.

3. **Result**: Your agent now calls Aegis instead of the MCP server directly. Aegis enforces ACL, rate limits, approval, and audit logging, then forwards to the backend.

### Architecture

```
Your Agent  →  Aegis (:18070)  →  Your MCP Server
```

## Multiple Agents

Aegis supports multiple agents with different permission levels. To add another agent, edit `config/aegis.yaml`:

```yaml
agents:
  my-agent:
    display_name: "Production Agent"
    backends:
      my-backend:
        allowed: true
        rate_limits:
          publish: { window: 24h, max_count: 5 }
        approval_required:
          - "publish"

  dev-agent:
    display_name: "Dev Agent"
    backends:
      my-backend:
        allowed: true
        tool_denylist: ["publish", "delete"]
```

Each agent gets its own URL: `http://localhost:18070/agents/{agent-id}/mcp`

Then reload without restarting:

```bash
curl -X POST localhost:18070/api/v1/config/reload
```

## Next Steps

- **[Policy Configuration Guide](policy-guide.md)** — Fine-tune ACL, rate limits, approval rules, and queue settings
- **[Management API](../README.md#management-api)** — Query audit logs, manage pending approvals, check rate limit usage
- **Hot reload** — Edit `aegis.yaml`, then `POST /api/v1/config/reload` — no restart needed
- **API authentication** — Set `server.api_token` in your config to protect management endpoints in production

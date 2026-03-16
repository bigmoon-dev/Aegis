# Getting Started with Aegis MCP — Claude Code

This guide walks you through protecting your MCP tool server with Aegis when using [Claude Code](https://docs.anthropic.com/en/docs/claude-code) as your AI agent.

**Time**: ~5 minutes

## Prerequisites

- Node.js >= 18 (for `npx` commands) or [download the binary](https://github.com/bigmoon-dev/Aegis/releases)
- A running MCP tool server you want to protect
- Claude Code installed (`claude` command available in terminal)

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
3. **Agent type** — Select **Claude Code** (auto-detected if installed). The wizard injects the Aegis proxy URL into `~/.claude/mcp_servers.json`.
4. **Approval notifications** (optional) — Configure a Feishu/Lark or generic webhook URL for approval request delivery. The wizard auto-detects your local IP for callback URLs.

### 2.2 Start Aegis

```bash
./aegis config/aegis.yaml
# or: npx aegis-mcp-proxy config/aegis.yaml
```

### 2.3 Verify

Claude Code auto-detects changes to `~/.claude/mcp_servers.json`. Start (or restart) Claude Code and call one of your protected tools — it now routes through Aegis.

```bash
# Check Aegis health
curl localhost:18070/health

# View audit log — you should see your tool call
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

### What Happened

The setup wizard performed these changes:

1. **Created** `config/aegis.yaml` — Aegis policy config with your backend, agent, and tool policies.

2. **Injected** an entry into `~/.claude/mcp_servers.json`:
   ```json
   {
     "your-backend": {
       "url": "http://localhost:18070/agents/claude-your-backend/mcp"
     }
   }
   ```
   A `.bak` backup was created before modification.

3. **Result**: Claude Code now calls Aegis instead of your MCP server directly. Aegis enforces ACL, rate limits, approval, and audit logging, then forwards to the backend.

### Architecture

```
Claude Code  →  Aegis (:18070)  →  Your MCP Server
```

## Next Steps

- **[Policy Configuration Guide](policy-guide.md)** — Fine-tune ACL, rate limits, approval rules, and queue settings
- **[Management API](../README.md#management-api)** — Query audit logs, manage pending approvals, check rate limit usage
- **Hot reload** — Edit `aegis.yaml`, then `POST /api/v1/config/reload` — no restart needed
- **API authentication** — Set `server.api_token` in your config to protect management endpoints in production

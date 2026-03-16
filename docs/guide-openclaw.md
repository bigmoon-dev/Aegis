# Getting Started with Aegis MCP — OpenClaw

This guide walks you through protecting your MCP tool server with Aegis when using [OpenClaw](https://github.com/nicepkg/openclaw) as your AI agent framework.

**Time**: ~10 minutes

## Prerequisites

- Node.js >= 18 (for `npx` commands) or [download the binary](https://github.com/bigmoon-dev/Aegis/releases)
- A running MCP tool server you want to protect
- OpenClaw installed with `mcporter` (`npm install -g mcporter`)
- OpenClaw gateway running (`openclaw gateway`)

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
3. **Agent type** — Select **OpenClaw** (auto-detected if installed). The wizard injects the Aegis proxy URL into `~/.openclaw/workspace/config/mcporter.json`.
4. **Approval notifications** (optional) — Configure a Feishu/Lark or generic webhook URL for approval request delivery. The wizard auto-detects your local IP for callback URLs.

### 2.2 Start Aegis

```bash
./aegis config/aegis.yaml
# or: npx aegis-mcp-proxy config/aegis.yaml
```

### 2.3 Restart OpenClaw

```bash
openclaw gateway restart
# or if using systemd:
systemctl --user restart openclaw-gateway
```

### 2.4 Verify

```bash
# Check Aegis health
curl localhost:18070/health

# View audit log — you should see tool calls after interacting with OpenClaw
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

### What Happened

The setup wizard performed these changes:

1. **Created** `config/aegis.yaml` — Aegis policy config with your backend, agent, and tool policies.

2. **Injected** an entry into `~/.openclaw/workspace/config/mcporter.json`:
   ```json
   {
     "mcpServers": {
       "your-backend": {
         "baseUrl": "http://localhost:18070/agents/openclaw-your-backend/mcp"
       }
     }
   }
   ```
   A `.bak` backup was created before modification.

3. **Result**: OpenClaw calls mcporter, which calls Aegis instead of your MCP server directly. Aegis enforces ACL, rate limits, approval, and audit logging, then forwards to the backend.

### Architecture

```
Feishu/Lark  →  OpenClaw  →  mcporter  →  Aegis (:18070)  →  Your MCP Server
```

## Troubleshooting: LLM Can't Call Tools Correctly?

OpenClaw uses `mcporter` to call MCP tools. The LLM must produce the correct `mcporter call` syntax, which can be tricky — common mistakes include missing the `call` subcommand, incorrect quoting of JSON arguments, or confusing server names.

### Solution: Wrapper Script + SKILL.md

Create a wrapper script that simplifies the call syntax, then describe it in a SKILL.md so the LLM knows how to use it.

**1. Create a wrapper script** (`~/.openclaw/workspace/tools/my-backend.sh`):

```bash
#!/bin/bash
# Usage: my-backend.sh <tool_name> <json_args>
# Example: my-backend.sh get_weather '{"city":"Beijing"}'
mcporter call my-backend "$1" "$2"
```

```bash
chmod +x ~/.openclaw/workspace/tools/my-backend.sh
```

**2. Describe it in SKILL.md** (`~/.openclaw/workspace/SKILL.md`):

```markdown
## MCP Tools (my-backend)

Call tools using the wrapper script:

    bash ~/.openclaw/workspace/tools/my-backend.sh <tool_name> '<json_args>'

Available tools:
- get_weather: Get weather for a city. Args: {"city": "string"}
- publish_post: Publish a post (requires approval). Args: {"title": "string", "content": "string"}
```

**3. Also add to TOOLS.md** (`~/.openclaw/workspace/TOOLS.md`) if your agent reads tool descriptions from there.

## Next Steps

- **[Policy Configuration Guide](policy-guide.md)** — Fine-tune ACL, rate limits, approval rules, and queue settings
- **[Management API](../README.md#management-api)** — Query audit logs, manage pending approvals, check rate limit usage
- **Hot reload** — Edit `aegis.yaml`, then `POST /api/v1/config/reload` — no restart needed
- **API authentication** — Set `server.api_token` in your config to protect management endpoints in production

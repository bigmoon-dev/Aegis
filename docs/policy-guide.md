# Policy Configuration Guide

This guide explains how to write policy rules in Aegis. All policies are defined in a single YAML configuration file.

## Table of Contents

- [Overview](#overview)
- [Backends](#backends)
- [Agent Policies](#agent-policies)
  - [Access Control (ACL)](#access-control-acl)
  - [Rate Limits](#rate-limits)
  - [Approval Rules](#approval-rules)
- [Queue Configuration](#queue-configuration)
  - [Global Rate Limits](#global-rate-limits)
  - [Bypass Tools](#bypass-tools)
- [How Policies Interact](#how-policies-interact)
- [Common Scenarios](#common-scenarios)
- [Reference](#reference)

## Overview

Aegis enforces policies through a **pipeline** that every `tools/call` request passes through:

```
ACL Check → Rate Limit Check → Approval Gate → FIFO Queue → Forward to Backend
```

Each stage can reject the request independently. Policies are configured per-agent and per-backend, giving you fine-grained control over what each agent can do.

**How to find tool names:** Connect any MCP client directly to your backend server and call `tools/list`. The tool names returned (e.g., `publish`, `search`, `delete`) are what you use in your policy configuration.

## Backends

Define the MCP tool servers that Aegis proxies to:

```yaml
backends:
  my-tools:                              # Backend ID (you choose this name)
    url: "http://localhost:8080/mcp"     # MCP endpoint URL
    health_url: "http://localhost:8080/health"  # Optional health check URL
    timeout: 120s                        # Request timeout
```

You can define multiple backends. Each agent can be granted access to different backends.

## Agent Policies

Each agent is identified by its ID in the URL path: `/agents/{agent-id}/mcp`. Define policies under the `agents` section:

```yaml
agents:
  my-agent:                              # Agent ID (used in URL path)
    display_name: "My Agent"             # Human-readable name (for logs and approval cards)
    backends:
      my-tools:                          # Must match a backend ID defined above
        allowed: true                    # Whether this agent can access this backend
        tool_denylist: [...]             # Tools to hide from this agent
        rate_limits: {...}               # Per-tool rate limits
        approval_required: [...]         # Tools requiring human approval
```

### Access Control (ACL)

ACL determines which tools an agent can see and call.

#### Block an entire backend

```yaml
agents:
  readonly-agent:
    backends:
      my-tools:
        allowed: false        # Agent cannot access any tools on this backend
```

#### Hide specific tools

```yaml
agents:
  dev-agent:
    backends:
      my-tools:
        allowed: true
        tool_denylist:        # These tools are removed from tools/list
          - "publish"         # Agent cannot see or call these tools
          - "delete"
```

**Key difference:**
- `allowed: false` — blocks the entire backend; the agent gets an error for any request
- `tool_denylist` — selectively hides specific tools; the agent can still use other tools on this backend

Denied tools are completely invisible to the agent — they are removed from the `tools/list` response, so the agent doesn't even know they exist.

### Rate Limits

Rate limits use a **sliding window** algorithm. Each tool can have its own window and count limit.

```yaml
agents:
  production-agent:
    backends:
      my-tools:
        rate_limits:
          publish: { window: 24h, max_count: 1 }    # 1 call per 24 hours
          like:    { window: 1h,  max_count: 10 }    # 10 calls per hour
          search:  { window: 30m, max_count: 5 }     # 5 calls per 30 minutes
```

#### Time format

Durations use Go's time format. Common values:

| Format | Meaning |
|--------|---------|
| `30s`  | 30 seconds |
| `5m`   | 5 minutes |
| `30m`  | 30 minutes |
| `1h`   | 1 hour |
| `2h`   | 2 hours |
| `24h`  | 24 hours |
| `168h` | 7 days |

#### How it works

- The sliding window counts successful calls within the past `window` duration
- **Only successful calls count** — failed or rejected calls don't consume quota
- When the limit is reached, the agent receives a JSON-RPC error (`-32002`) with a message explaining the limit
- Rate limit records are persisted in SQLite, so limits survive server restarts

#### Tool description annotations

Rate limits are automatically injected into tool descriptions visible to the agent:

```
Original:  "Publish a post"
Enhanced:  "[Rate:1/1d] Publish a post"
```

This helps the agent understand its constraints before making decisions.

### Approval Rules

Specify which tools require human approval before execution:

```yaml
agents:
  production-agent:
    backends:
      my-tools:
        approval_required:
          - "publish"
          - "delete"
```

When an agent calls an approval-required tool:
1. Aegis sends a notification to Feishu/Lark with an interactive card
2. The card shows the agent name, tool name, and argument preview
3. A human clicks **Approve** or **Reject**
4. If approved, the request enters the FIFO queue for execution
5. If rejected or timed out (default 10 minutes), the agent receives an error (`-32004`)

Configure the approval system:

```yaml
approval:
  feishu:
    webhook_url: "https://open.feishu.cn/open-apis/bot/v2/hook/your-token"
  timeout: 600s                          # 10 minutes, auto-reject after this
  callback_base_url: "http://your-server:18070"  # Must be reachable by the approver's browser
```

Tools with both rate limits and approval show combined annotations:

```
"[Rate:1/1d|ApprovalRequired] Publish a post"
```

## Queue Configuration

The FIFO queue serializes operations per backend. All agents sharing the same backend go through the same queue.

```yaml
queue:
  my-tools:                    # Must match a backend ID
    enabled: true
    delay_min: 60s             # Minimum delay between operations (randomized)
    delay_max: 600s            # Maximum delay between operations (randomized)
    max_pending: 50            # Maximum queue size; new requests are rejected when full
```

The random delay between operations mimics human behavior patterns, reducing the risk of platform detection.

### Global Rate Limits

Global rate limits apply **across all agents** for a given backend. This is important when multiple agents share the same account.

```yaml
queue:
  my-tools:
    global_rate_limits:
      login_check:  { window: 2h, max_count: 6 }   # All agents combined: 6 per 2 hours
      search:       { window: 1h, max_count: 15 }   # All agents combined: 15 per hour
```

**Check order:** Global limits are checked first, then per-agent limits. If either is exceeded, the request is rejected.

**Example:** If Agent A has `search: { window: 1h, max_count: 10 }` and the global limit is `search: { window: 1h, max_count: 15 }`, then:
- Agent A can search at most 10 times per hour (per-agent limit)
- All agents combined can search at most 15 times per hour (global limit)
- If Agent A searches 8 times and Agent B searches 7 times, Agent B's next search is rejected by the global limit even though Agent B's per-agent limit isn't reached

Tools with global rate limits show combined annotations:

```
"[Rate:10/1h|GlobalRate:15/1h] Search posts"
```

### Bypass Tools

Some tools (e.g., health checks, status queries) don't need to wait in the queue:

```yaml
queue:
  my-tools:
    bypass_tools:
      - "health_check"
      - "get_status"
```

**Important:** Bypass only skips the FIFO queue. These tools still go through ACL checks and rate limiting.

## How Policies Interact

Here's the complete flow when an agent calls a tool:

```
1. ACL Check
   ├── Backend allowed?     → No  → Error -32001
   └── Tool in denylist?    → Yes → Error -32001

2. Rate Limit Check
   ├── Global limit exceeded?    → Yes → Error -32002
   └── Per-agent limit exceeded? → Yes → Error -32002

3. Approval Gate
   └── Tool in approval_required? → Yes → Send notification, wait for human
       ├── Approved  → Continue
       ├── Rejected  → Error -32004
       └── Timeout   → Error -32004

4. FIFO Queue
   ├── Tool in bypass_tools? → Yes → Execute immediately
   └── No → Enter queue
       ├── Queue full (> max_pending)? → Error -32003
       └── Wait for turn → Execute → Random delay before next item

5. Forward to Backend → Return result

6. Audit Log (records everything regardless of outcome)
```

## Common Scenarios

### Scenario 1: Production agent with full controls

```yaml
agents:
  production:
    display_name: "Production Agent"
    backends:
      my-tools:
        allowed: true
        tool_denylist:
          - "debug_tool"
          - "reset_data"
        rate_limits:
          publish:  { window: 24h, max_count: 1 }
          like:     { window: 1h,  max_count: 10 }
          comment:  { window: 1h,  max_count: 3 }
          search:   { window: 1h,  max_count: 10 }
        approval_required:
          - "publish"
          - "delete"
```

### Scenario 2: Development agent (read-only)

```yaml
agents:
  dev:
    display_name: "Dev Agent"
    backends:
      my-tools:
        allowed: true
        tool_denylist:
          - "publish"
          - "delete"
          - "like"
          - "comment"
        rate_limits:
          search: { window: 1h, max_count: 5 }
```

### Scenario 3: Completely blocked agent

```yaml
agents:
  blocked:
    display_name: "Blocked Agent"
    backends:
      my-tools:
        allowed: false
```

### Scenario 4: Multiple agents sharing one account with global limits

```yaml
queue:
  social-media:
    enabled: true
    delay_min: 60s
    delay_max: 300s
    global_rate_limits:
      post:    { window: 24h, max_count: 3 }    # All agents: max 3 posts/day
      search:  { window: 1h,  max_count: 20 }   # All agents: max 20 searches/hour

agents:
  agent-a:
    backends:
      social-media:
        allowed: true
        rate_limits:
          post:   { window: 24h, max_count: 1 }   # This agent: max 1 post/day
          search: { window: 1h,  max_count: 10 }   # This agent: max 10 searches/hour

  agent-b:
    backends:
      social-media:
        allowed: true
        rate_limits:
          post:   { window: 24h, max_count: 2 }
          search: { window: 1h,  max_count: 10 }
```

## Reference

### Configuration fields

| Field | Type | Description |
|-------|------|-------------|
| `backends.{id}.url` | string | MCP server endpoint URL |
| `backends.{id}.health_url` | string | Optional health check endpoint |
| `backends.{id}.timeout` | duration | Request timeout |
| `agents.{id}.display_name` | string | Human-readable agent name |
| `agents.{id}.backends.{id}.allowed` | bool | Whether the agent can access this backend |
| `agents.{id}.backends.{id}.tool_denylist` | []string | Tools hidden from this agent |
| `agents.{id}.backends.{id}.rate_limits.{tool}` | object | `{ window: duration, max_count: int }` |
| `agents.{id}.backends.{id}.approval_required` | []string | Tools requiring human approval |
| `queue.{id}.enabled` | bool | Enable FIFO queue for this backend |
| `queue.{id}.delay_min` | duration | Minimum random delay between operations |
| `queue.{id}.delay_max` | duration | Maximum random delay between operations |
| `queue.{id}.max_pending` | int | Maximum queue size |
| `queue.{id}.bypass_tools` | []string | Tools that skip the queue |
| `queue.{id}.global_rate_limits.{tool}` | object | `{ window: duration, max_count: int }` |
| `approval.feishu.webhook_url` | string | Feishu/Lark webhook URL |
| `approval.timeout` | duration | Auto-reject timeout |
| `approval.callback_base_url` | string | Base URL for approval callback buttons |
| `audit.db_path` | string | SQLite database file path |
| `audit.retention_days` | int | Auto-purge records older than this |

### JSON-RPC error codes

| Code | Meaning |
|------|---------|
| `-32001` | ACL denied (agent not allowed to call this tool) |
| `-32002` | Rate limited (global or per-agent limit exceeded) |
| `-32003` | Queue full (too many pending operations) |
| `-32004` | Approval denied or timed out |

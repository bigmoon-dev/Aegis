# My AI Agent Got an Account Banned. So I Built an MCP Proxy.

Last month I was running an AI agent for social media automation. The agent talked to an MCP tool server — it could publish posts, query analytics, delete content.

I put "max 1 post per day, ask me before publishing" in the system prompt.

The LLM decided that "ask me" meant "I've already considered this" in one of its reasoning chains. Eight posts in 30 minutes. Account banned.

If you've run agents on real accounts, you've probably hit some version of this:
- Prompt says "max 3 calls" — agent makes a 4th
- Prompt says "read-only" — agent constructs a write call anyway
- Prompt says "get approval" — agent doesn't know what approval means, it just calls tools

**Prompts are suggestions. LLMs are not rule engines.**

## The gap

MCP has no governance layer. An agent gets `tools/list`, sees 5 tools, and assumes all 5 are fair game. Nothing at the protocol level says "you can't use this tool" or "this tool needs human sign-off" or "you've used this tool too many times today."

All constraints live in the prompt. Prompts are text. LLMs are probabilistic. Probabilistic model + text constraints = occasional failure.

Occasional failure is fine for chatbots. It's not fine when your agent is operating a real social media account, e-commerce store, or customer service system. One burst of unchecked actions can trigger a platform ban.

## The fix: a governance proxy at the MCP protocol level

I built [Aegis MCP](https://github.com/bigmoon-dev/Aegis) — a proxy that sits between your agent and your MCP tool server.

The idea is simple. Instead of connecting directly to the backend, the agent connects to Aegis. Aegis intercepts every request and runs it through a policy pipeline before deciding whether to forward it:

```
Agent → Aegis (:18070) → MCP Tool Server
```

The pipeline:

1. **ACL** — Controls which tools each agent can see. Denied tools are removed from `tools/list` responses. The agent doesn't even know they exist.
2. **Rate limiting** — Sliding window. Per-agent limits AND cross-agent global limits. Multiple agents sharing one account? Global limits prevent cumulative overuse.
3. **Human approval** — Destructive operations (publish, delete) are held until a human approves via webhook notification (Feishu/Lark, Slack, any HTTP endpoint). No approval = request stays pending forever.
4. **FIFO queue** — Serialized per-backend execution with randomized delays between operations. Mimics human timing patterns.
5. **Audit log** — Every call recorded to SQLite: agent, tool, arguments, verdict, duration, result.

The key point: **these are hard constraints**. No matter what the LLM decides or how many times the agent retries — exceed the rate limit and you get `-32002`. No approval means the request hangs. The agent can't bypass it.

## Try it

Fastest way to see it work:

```bash
npx aegis-mcp-proxy demo
```

This spins up a mock tool server + Aegis with pre-configured policies. Terminal prints curl commands to try:
- `echo` passes through, no restrictions
- `get_weather` × 4 — 4th call gets rate-limited
- `publish_post` — blocks until you approve via API
- `admin_reset` — invisible in tools/list (ACL denied)

To protect your own MCP server:

```bash
npx aegis-mcp-proxy setup
```

The wizard connects to your server, discovers tools, suggests policies based on tool names (read-only = unlimited, publish = rate limit + approval, dangerous = deny), and injects the proxy URL into your agent's config (auto-detects Claude Code and OpenClaw).

Config changes don't need a restart:

```bash
curl -X POST localhost:18070/api/v1/config/reload
```

## Why a proxy, not an SDK?

SDK approaches require code changes in every agent. Switch frameworks and you re-integrate. A protocol-level proxy means the agent just changes one URL — zero code changes. Works with Claude Code, OpenClaw, or any MCP-compatible agent.

## Why SQLite, not Redis?

I wanted a single binary with no external dependencies. Audit logs need persistence anyway. The whole project has 3 dependencies (SQLite, YAML, UUID). It runs on a Raspberry Pi.

## Why inject constraints into tool descriptions?

When the agent calls `tools/list`, Aegis adds constraint info to each tool's description: `[Rate:1/1d|ApprovalRequired] Publish post`. The LLM sees limits before deciding to call the tool, which reduces pointless retries.

## Current state

- Written in Go, single binary, 90%+ test coverage (185 tests)
- Available via npm / Docker / go install / GitHub Releases
- Apache 2.0

Code: [github.com/bigmoon-dev/Aegis](https://github.com/bigmoon-dev/Aegis)

If you're running AI agents on real accounts, give it a try. Issues and feedback welcome.

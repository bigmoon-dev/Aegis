# Aegis

**基于 [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) 的 AI Agent 治理代理。**

[English](README.md) | 中文

Aegis 位于 AI Agent 与 MCP 工具服务器之间，在协议层实施 Agent 无法绕过的硬约束 — 频率限制、访问控制、人工审批、串行执行队列和完整审计日志。

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

## 为什么需要 Aegis

AI Agent 能力强大，但不是可靠的规则执行者。基于 prompt 的"软规则"（如"每天最多发一篇"）经常被违反。当 Agent 操作真实账号 — 社交媒体、电商、客服 — 一次失控的操作就可能触发平台封禁、合规违规，甚至更严重的后果。

Aegis 将软规则转化为**协议层的程序化硬约束**。无论 LLM 做出什么决策，Agent 都无法超出频率限制或跳过审批步骤。

## 功能特性

- **访问控制 (ACL)** — 按 Agent、后端、工具粒度的 allow/deny 规则。被禁止的工具对 Agent 不可见（从 `tools/list` 响应中移除）。

- **两级频率限制** — 单 Agent 滑动窗口限制 *和* 跨 Agent 全局限制。多个 Agent 共享同一账号？全局限制防止累积超频。

- **人工审批流程** — 破坏性操作（发布、删除）需通过飞书/Lark Webhook 通知获取人工审批，回调 URL 采用 HMAC 签名。可配置超时时间，超时自动拒绝。

- **FIFO 执行队列** — 按后端串行执行，操作间随机延迟（1-10 分钟，可配置），模拟人类操作节奏。只读工具可配置跳过队列。

- **审计日志** — 每次工具调用均记录到 SQLite：Agent、工具、参数、ACL/限流/审批结果、队列位置、执行耗时、返回结果。支持自动清理和可配置的保留天数。

- **工具描述增强** — 约束信息注入到工具描述中，Agent 看到的是 `[Rate:1/1d|ApprovalRequired] 发布笔记` 而非 `发布笔记`。Agent 在决策前就能感知自身限制。

- **热更新** — 通过 `POST /api/v1/config/reload` 更新配置，无需重启服务。

- **单二进制部署** — Go 编写，仅 3 个依赖（SQLite、YAML、UUID），可在树莓派上运行。

## 快速开始

```bash
# 构建
make build

# 初始化配置
make init-config
# 编辑 config/aegis.yaml 填入你的配置

# 运行
./aegis config/aegis.yaml
```

将 MCP 客户端指向 `http://localhost:18070/agents/{agent-id}/mcp`，替代直接连接后端。

### 交叉编译（树莓派）

```bash
# 需要交叉编译器（CGO/SQLite 依赖），例如 aarch64-linux-gnu-gcc
CC=aarch64-linux-gnu-gcc make cross-rpi
scp aegis user@your-server:~/aegis/

# 或直接在目标机器上构建：
ssh user@your-server 'cd ~/aegis && make build'
```

## 配置

```yaml
server:
  listen: ":18070"
  read_timeout: 300s
  write_timeout: 300s

backends:
  my-tools:
    url: "http://localhost:8080/mcp"    # 你的 MCP 工具服务器
    health_url: "http://localhost:8080/health"
    timeout: 120s

queue:
  my-tools:
    enabled: true
    delay_min: 60s                      # 操作间最小延迟
    delay_max: 600s                     # 操作间最大延迟
    max_pending: 50
    bypass_tools:                       # 跳过队列（仍受限流约束）
      - "health_check"
    global_rate_limits:                 # 跨所有 Agent 的全局限制
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
    webhook_url: ""                     # 你的飞书 Webhook URL
  timeout: 600s
  callback_base_url: "http://your-server:18070"

audit:
  db_path: "./data/audit.db"
  retention_days: 90
```

## 处理管道

每个 `tools/call` 请求依次经过：

| 阶段 | 职责 | 拒绝时 |
|------|------|--------|
| **ACL** | Agent 是否有权调用此工具？ | JSON-RPC `-32001` |
| **频率限制** | 全局 + 单 Agent 滑动窗口检查 | JSON-RPC `-32002` |
| **审批门控** | 通过 Webhook 通知请求人工审批 | JSON-RPC `-32004`（超时） |
| **FIFO 队列** | 随机延迟的串行执行 | JSON-RPC `-32003`（队列满） |
| **转发器** | 代理到后端 MCP 服务器 | 后端错误 |
| **审计日志** | 记录所有操作到 SQLite | — |

仅成功的调用计入频率限制（失败调用不消耗配额）。

## 管理 API

```
GET  /health                           # 服务 + 后端健康检查
GET  /api/v1/queue/status              # 各后端的队列状态
GET  /api/v1/agents                    # Agent 列表与权限
GET  /api/v1/agents/{id}/rate-limits   # 当前用量与限额（Agent + 全局维度）
GET  /api/v1/approvals/pending         # 待审批列表
POST /api/v1/approvals/{id}/approve    # 通过 API 批准
POST /api/v1/approvals/{id}/reject     # 通过 API 拒绝
GET  /api/v1/audit/logs                # 查询审计日志（?limit=50&offset=0）
POST /api/v1/config/reload             # 热更新配置
```

## 工作原理

1. Agent 发送 MCP 请求到 `http://aegis:18070/agents/{agent-id}/mcp`
2. Aegis 从 URL 路径识别 Agent 身份
3. `tools/list` → 从后端获取工具列表，过滤禁止的工具，注入约束标注
4. `tools/call` → 经过完整管道处理（ACL → 限流 → 审批 → 队列 → 转发 → 审计）
5. `initialize`、`ping` 等 → 透明转发
6. Agent 只能看到被允许的工具，只能执行被允许的操作

## 设计决策

| 决策 | 理由 |
|------|------|
| MCP 代理模式（非 SDK Server） | 动态转发工具；无需修改上游源码 |
| SQLite（非 Redis） | 最小化依赖；持久化审计记录；低资源占用 |
| 按后端分队列 | 共享同一账号的所有 Agent 必须全局串行 |
| HMAC 签名的审批回调 | 防止通过 URL 猜测进行未授权审批 |
| 全程 UTC 时间 | 避免夏令时切换导致限流窗口偏差 |
| 全局 + 单 Agent 频率限制 | 多 Agent 共享账号的累积频率控制 |

## 环境要求

- Go 1.22+（需启用 CGO 以支持 SQLite）
- 一个兼容 MCP 协议的工具服务器作为后端

## 许可证

[AGPL-3.0](LICENSE)

## 致谢

为治理在真实平台上运行的 AI Agent 而构建，源自社交媒体自动化风控的实战教训。

# 策略配置指南

本指南说明如何在 Aegis 中编写策略规则。所有策略均在一个 YAML 配置文件中定义。

## 目录

- [概述](#概述)
- [后端配置](#后端配置)
- [Agent 策略](#agent-策略)
  - [访问控制 (ACL)](#访问控制-acl)
  - [频率限制](#频率限制)
  - [审批规则](#审批规则)
- [队列配置](#队列配置)
  - [全局频率限制](#全局频率限制)
  - [队列旁路工具](#队列旁路工具)
- [策略交互关系](#策略交互关系)
- [常见场景](#常见场景)
- [参考](#参考)

## 概述

Aegis 通过 **处理管道** 来执行策略，每个 `tools/call` 请求都会依次经过：

```
ACL 检查 → 频率限制检查 → 审批门控 → FIFO 队列 → 转发到后端
```

每个阶段都可以独立拒绝请求。策略按 Agent 和后端维度配置，提供细粒度的控制能力。

**如何获取工具名称：** 将任意 MCP 客户端直接连接到你的后端服务器，调用 `tools/list`。返回的工具名（如 `publish`、`search`、`delete`）就是策略配置中使用的名称。

## 后端配置

定义 Aegis 代理的 MCP 工具服务器：

```yaml
backends:
  my-tools:                              # 后端 ID（自定义名称）
    url: "http://localhost:8080/mcp"     # MCP 端点 URL
    health_url: "http://localhost:8080/health"  # 可选的健康检查 URL
    timeout: 120s                        # 请求超时时间
```

你可以定义多个后端，每个 Agent 可以被授权访问不同的后端。

## Agent 策略

每个 Agent 通过 URL 路径中的 ID 标识：`/agents/{agent-id}/mcp`。在 `agents` 段落下定义策略：

```yaml
agents:
  my-agent:                              # Agent ID（用于 URL 路径）
    display_name: "My Agent"             # 可读名称（用于日志和审批卡片）
    backends:
      my-tools:                          # 必须匹配上面定义的后端 ID
        allowed: true                    # 是否允许该 Agent 访问此后端
        tool_denylist: [...]             # 对该 Agent 隐藏的工具
        rate_limits: {...}               # 按工具的频率限制
        approval_required: [...]         # 需要人工审批的工具
```

### 访问控制 (ACL)

ACL 决定 Agent 可以看到和调用哪些工具。

#### 禁止访问整个后端

```yaml
agents:
  readonly-agent:
    backends:
      my-tools:
        allowed: false        # Agent 无法访问此后端的任何工具
```

#### 隐藏特定工具

```yaml
agents:
  dev-agent:
    backends:
      my-tools:
        allowed: true
        tool_denylist:        # 这些工具从 tools/list 中移除
          - "publish"         # Agent 无法看到或调用这些工具
          - "delete"
```

**关键区别：**
- `allowed: false` — 禁止访问整个后端；Agent 的任何请求都会收到错误
- `tool_denylist` — 选择性隐藏特定工具；Agent 仍可使用该后端的其他工具

被禁止的工具对 Agent 完全不可见 — 它们会从 `tools/list` 响应中移除，Agent 甚至不知道这些工具的存在。

### 频率限制

频率限制使用 **滑动窗口** 算法。每个工具可以有独立的窗口和次数限制。

```yaml
agents:
  production-agent:
    backends:
      my-tools:
        rate_limits:
          publish: { window: 24h, max_count: 1 }    # 24 小时内最多 1 次
          like:    { window: 1h,  max_count: 10 }    # 每小时最多 10 次
          search:  { window: 30m, max_count: 5 }     # 每 30 分钟最多 5 次
```

#### 时间格式

使用 Go 的时间格式，常用值：

| 格式 | 含义 |
|------|------|
| `30s`  | 30 秒 |
| `5m`   | 5 分钟 |
| `30m`  | 30 分钟 |
| `1h`   | 1 小时 |
| `2h`   | 2 小时 |
| `24h`  | 24 小时 |
| `168h` | 7 天 |

#### 工作原理

- 滑动窗口统计过去 `window` 时间内的成功调用次数
- **仅统计成功调用** — 失败或被拒绝的调用不消耗配额
- 达到限制时，Agent 收到 JSON-RPC 错误（`-32002`），附带限制说明
- 频率记录持久化在 SQLite 中，服务重启后限制不会重置

#### 工具描述标注

频率限制会自动注入到 Agent 可见的工具描述中：

```
原始：  "Publish a post"
增强：  "[Rate:1/1d] Publish a post"
```

这帮助 Agent 在决策前了解自身约束。

### 审批规则

指定哪些工具需要人工审批后才能执行：

```yaml
agents:
  production-agent:
    backends:
      my-tools:
        approval_required:
          - "publish"
          - "delete"
```

当 Agent 调用需要审批的工具时：
1. Aegis 向飞书/Lark 发送交互式卡片通知
2. 卡片显示 Agent 名称、工具名和参数预览
3. 人工点击 **Approve** 或 **Reject**
4. 批准后请求进入 FIFO 队列等待执行
5. 被拒绝或超时（默认 10 分钟）后，Agent 收到错误（`-32004`）

配置审批系统：

```yaml
approval:
  feishu:
    webhook_url: "https://open.feishu.cn/open-apis/bot/v2/hook/your-token"
  timeout: 600s                          # 10 分钟，超时自动拒绝
  callback_base_url: "http://your-server:18070"  # 审批者浏览器必须能访问此地址
```

同时配置了频率限制和审批的工具会显示组合标注：

```
"[Rate:1/1d|ApprovalRequired] Publish a post"
```

## 队列配置

FIFO 队列按后端串行化操作。共享同一后端的所有 Agent 进入同一队列。

```yaml
queue:
  my-tools:                    # 必须匹配后端 ID
    enabled: true
    delay_min: 60s             # 操作间最小延迟（随机化）
    delay_max: 600s            # 操作间最大延迟（随机化）
    max_pending: 50            # 最大队列长度；队列满时新请求被拒绝
```

操作间的随机延迟模拟人类行为模式，降低被平台检测的风险。

### 全局频率限制

全局频率限制应用于 **所有 Agent**（针对同一后端）。当多个 Agent 共享同一账号时，这一点至关重要。

```yaml
queue:
  my-tools:
    global_rate_limits:
      login_check:  { window: 2h, max_count: 6 }   # 所有 Agent 合计：2 小时内最多 6 次
      search:       { window: 1h, max_count: 15 }   # 所有 Agent 合计：每小时最多 15 次
```

**检查顺序：** 先检查全局限制，再检查单 Agent 限制。任一超限即拒绝。

**示例：** 假设 Agent A 的限制为 `search: { window: 1h, max_count: 10 }`，全局限制为 `search: { window: 1h, max_count: 15 }`：
- Agent A 每小时最多搜索 10 次（单 Agent 限制）
- 所有 Agent 合计每小时最多搜索 15 次（全局限制）
- 如果 Agent A 搜索了 8 次、Agent B 搜索了 7 次，Agent B 的下一次搜索会被全局限制拒绝，即使 Agent B 自身的限制还没用完

带全局限制的工具显示组合标注：

```
"[Rate:10/1h|GlobalRate:15/1h] Search posts"
```

### 队列旁路工具

某些工具（如健康检查、状态查询）不需要在队列中等待：

```yaml
queue:
  my-tools:
    bypass_tools:
      - "health_check"
      - "get_status"
```

**注意：** 旁路仅跳过 FIFO 队列，这些工具仍然经过 ACL 检查和频率限制。

## 策略交互关系

Agent 调用工具时的完整流程：

```
1. ACL 检查
   ├── 后端已允许？      → 否 → 错误 -32001
   └── 工具在禁止列表？   → 是 → 错误 -32001

2. 频率限制检查
   ├── 全局限制超额？     → 是 → 错误 -32002
   └── Agent 限制超额？   → 是 → 错误 -32002

3. 审批门控
   └── 工具需要审批？     → 是 → 发送通知，等待人工
       ├── 已批准  → 继续
       ├── 已拒绝  → 错误 -32004
       └── 超时    → 错误 -32004

4. FIFO 队列
   ├── 工具在 bypass_tools 中？ → 是 → 立即执行
   └── 否 → 进入队列
       ├── 队列满（> max_pending）？ → 错误 -32003
       └── 等待轮次 → 执行 → 随机延迟后处理下一个

5. 转发到后端 → 返回结果

6. 审计日志（无论结果如何，记录所有操作）
```

## 常见场景

### 场景 1：带完整管控的生产 Agent

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

### 场景 2：开发 Agent（只读）

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

### 场景 3：完全禁止的 Agent

```yaml
agents:
  blocked:
    display_name: "Blocked Agent"
    backends:
      my-tools:
        allowed: false
```

### 场景 4：多 Agent 共享账号 + 全局限制

```yaml
queue:
  social-media:
    enabled: true
    delay_min: 60s
    delay_max: 300s
    global_rate_limits:
      post:    { window: 24h, max_count: 3 }    # 所有 Agent 合计：每天最多 3 篇
      search:  { window: 1h,  max_count: 20 }   # 所有 Agent 合计：每小时最多 20 次

agents:
  agent-a:
    backends:
      social-media:
        allowed: true
        rate_limits:
          post:   { window: 24h, max_count: 1 }   # 此 Agent：每天最多 1 篇
          search: { window: 1h,  max_count: 10 }   # 此 Agent：每小时最多 10 次

  agent-b:
    backends:
      social-media:
        allowed: true
        rate_limits:
          post:   { window: 24h, max_count: 2 }
          search: { window: 1h,  max_count: 10 }
```

## 参考

### 配置字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `backends.{id}.url` | string | MCP 服务器端点 URL |
| `backends.{id}.health_url` | string | 可选的健康检查端点 |
| `backends.{id}.timeout` | duration | 请求超时时间 |
| `agents.{id}.display_name` | string | 可读的 Agent 名称 |
| `agents.{id}.backends.{id}.allowed` | bool | 是否允许访问此后端 |
| `agents.{id}.backends.{id}.tool_denylist` | []string | 对此 Agent 隐藏的工具 |
| `agents.{id}.backends.{id}.rate_limits.{tool}` | object | `{ window: duration, max_count: int }` |
| `agents.{id}.backends.{id}.approval_required` | []string | 需要人工审批的工具 |
| `queue.{id}.enabled` | bool | 是否启用此后端的 FIFO 队列 |
| `queue.{id}.delay_min` | duration | 操作间最小随机延迟 |
| `queue.{id}.delay_max` | duration | 操作间最大随机延迟 |
| `queue.{id}.max_pending` | int | 最大队列长度 |
| `queue.{id}.bypass_tools` | []string | 跳过队列的工具 |
| `queue.{id}.global_rate_limits.{tool}` | object | `{ window: duration, max_count: int }` |
| `approval.feishu.webhook_url` | string | 飞书/Lark Webhook URL |
| `approval.timeout` | duration | 自动拒绝超时时间 |
| `approval.callback_base_url` | string | 审批回调按钮的基础 URL |
| `audit.db_path` | string | SQLite 数据库文件路径 |
| `audit.retention_days` | int | 自动清理超过此天数的记录 |

### JSON-RPC 错误码

| 错误码 | 含义 |
|--------|------|
| `-32001` | ACL 拒绝（Agent 无权调用此工具） |
| `-32002` | 频率受限（全局或单 Agent 限制超额） |
| `-32003` | 队列已满（待处理操作过多） |
| `-32004` | 审批被拒绝或超时 |

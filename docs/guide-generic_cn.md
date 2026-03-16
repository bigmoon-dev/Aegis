# Aegis MCP 接入指南 — 通用 Agent

本指南将引导你使用任何 MCP 兼容 Agent 时，通过 Aegis 保护你的 MCP 工具服务器。

**耗时**：约 5 分钟

## 前提条件

- Node.js >= 18（用于 `npx` 命令）或[下载预编译二进制](https://github.com/bigmoon-dev/Aegis/releases)
- 一个正在运行的 MCP 工具服务器
- 一个支持自定义 MCP 服务器 URL 的 MCP 兼容 Agent

## 第一步：体验 Demo

在接入自己的服务器之前，先通过交互式 Demo 体验 Aegis 的全部功能。

### 安装并运行

```bash
npx aegis-mcp-proxy demo
```

这会启动一个 mock MCP 服务器和 Aegis 代理（内置预配置策略），终端会打印 curl 命令供你逐步体验。

### 你会看到什么

| 命令 | 效果 |
|------|------|
| `tools/list` | 发现 5 个 mock 工具，`admin_reset` 被 ACL 隐藏，只显示 4 个 |
| `echo` | 无任何限制，直接通过 |
| `get_weather` x4 | 前 3 次成功，第 4 次被限流拦截（3次/分钟） |
| `publish_post` | 阻塞等待人工审批（通过管理 API 批准） |
| `list_posts` | 旁路 FIFO 队列，立即返回 |
| `audit/logs` | 查看所有操作的完整审计日志 |

### 验证

```bash
# 健康检查
curl localhost:18070/health

# 查看审计日志
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

体验完成后，按 `Ctrl+C` 停止 Demo。

## 第二步：保护你的 MCP 服务器

### 2.1 运行配置向导

```bash
aegis setup
# 或: npx aegis-mcp-proxy setup
```

按照提示操作：

1. **后端地址** — 输入你的 MCP 服务器地址（例如 `http://localhost:9200/mcp`）。Aegis 会自动连接并发现可用工具。
2. **逐工具策略** — 查看根据工具名称智能推荐的默认策略。只读工具不限制；发布/写入工具自动加限流 + 审批；危险工具默认禁止。
3. **Agent 类型** — 选择 **Custom（自定义）**。输入你的 Agent ID（例如 `my-agent`），它将成为 Aegis 代理 URL 的一部分。
4. **审批通知**（可选）— 配置飞书/Lark 或通用 Webhook URL 用于审批请求推送。向导会自动检测本机 IP 作为回调地址。

### 2.2 启动 Aegis

```bash
./aegis config/aegis.yaml
# 或: npx aegis-mcp-proxy config/aegis.yaml
```

### 2.3 更新你的 Agent 配置

将 Agent 的 MCP 服务器 URL 从直连后端改为 Aegis 代理地址：

```
# 改之前（直连后端）
http://localhost:9200/mcp

# 改之后（经过 Aegis）
http://localhost:18070/agents/my-agent/mcp
```

将 `my-agent` 替换为你在向导中选择的 Agent ID，`9200` 替换为你实际的后端端口。

### 2.4 验证

```bash
# 检查 Aegis 健康状态
curl localhost:18070/health

# 查看审计日志 — Agent 与后端交互后应能看到调用记录
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

### 发生了什么

配置向导执行了以下操作：

1. **创建了** `config/aegis.yaml` — Aegis 策略配置文件，包含你的后端、Agent 和工具策略。

2. **输出了**代理 URL 供手动配置：
   ```
   http://localhost:18070/agents/my-agent/mcp
   ```
   将你的 Agent 指向此 URL，替代直连后端。

3. **效果**：你的 Agent 现在调用 Aegis 而非直接调用 MCP 服务器。Aegis 负责执行 ACL、限流、审批和审计日志，然后转发到后端。

### 架构

```
你的 Agent  →  Aegis (:18070)  →  你的 MCP 服务器
```

## 多 Agent 配置

Aegis 支持多个 Agent 使用不同的权限级别。要添加新 Agent，编辑 `config/aegis.yaml`：

```yaml
agents:
  my-agent:
    display_name: "生产 Agent"
    backends:
      my-backend:
        allowed: true
        rate_limits:
          publish: { window: 24h, max_count: 5 }
        approval_required:
          - "publish"

  dev-agent:
    display_name: "开发 Agent"
    backends:
      my-backend:
        allowed: true
        tool_denylist: ["publish", "delete"]
```

每个 Agent 使用各自的 URL：`http://localhost:18070/agents/{agent-id}/mcp`

然后无需重启即可热更新：

```bash
curl -X POST localhost:18070/api/v1/config/reload
```

## 下一步

- **[策略配置指南](policy-guide_cn.md)** — 精细调整 ACL、限流、审批规则和队列设置
- **[管理 API](../README_CN.md#管理-api)** — 查询审计日志、管理待审批请求、查看限流用量
- **热更新** — 编辑 `aegis.yaml`，然后 `POST /api/v1/config/reload` — 无需重启
- **API 认证** — 在配置中设置 `server.api_token`，保护生产环境的管理端点

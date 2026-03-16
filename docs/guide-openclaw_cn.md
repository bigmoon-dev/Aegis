# Aegis MCP 接入指南 — OpenClaw

本指南将引导你使用 [OpenClaw](https://github.com/nicepkg/openclaw) 作为 AI Agent 框架时，通过 Aegis 保护你的 MCP 工具服务器。

**耗时**：约 10 分钟

## 前提条件

- Node.js >= 18（用于 `npx` 命令）或[下载预编译二进制](https://github.com/bigmoon-dev/Aegis/releases)
- 一个正在运行的 MCP 工具服务器
- 已安装 OpenClaw 和 `mcporter`（`npm install -g mcporter`）
- OpenClaw 网关已运行（`openclaw gateway`）

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
3. **Agent 类型** — 选择 **OpenClaw**（如已安装会自动检测）。向导会将 Aegis 代理地址注入 `~/.openclaw/workspace/config/mcporter.json`。
4. **审批通知**（可选）— 配置飞书/Lark 或通用 Webhook URL 用于审批请求推送。向导会自动检测本机 IP 作为回调地址。

### 2.2 启动 Aegis

```bash
./aegis config/aegis.yaml
# 或: npx aegis-mcp-proxy config/aegis.yaml
```

### 2.3 重启 OpenClaw

```bash
openclaw gateway restart
# 或使用 systemd:
systemctl --user restart openclaw-gateway
```

### 2.4 验证

```bash
# 检查 Aegis 健康状态
curl localhost:18070/health

# 查看审计日志 — 与 OpenClaw 交互后应能看到工具调用记录
curl 'localhost:18070/api/v1/audit/logs?limit=5' | jq
```

### 发生了什么

配置向导执行了以下操作：

1. **创建了** `config/aegis.yaml` — Aegis 策略配置文件，包含你的后端、Agent 和工具策略。

2. **注入了**一条记录到 `~/.openclaw/workspace/config/mcporter.json`：
   ```json
   {
     "mcpServers": {
       "your-backend": {
         "baseUrl": "http://localhost:18070/agents/openclaw-your-backend/mcp"
       }
     }
   }
   ```
   修改前会自动创建 `.bak` 备份。

3. **效果**：OpenClaw 通过 mcporter 调用 Aegis 而非直接调用 MCP 服务器。Aegis 负责执行 ACL、限流、审批和审计日志，然后转发到后端。

### 架构

```
飞书/Feishu  →  OpenClaw  →  mcporter  →  Aegis (:18070)  →  你的 MCP 服务器
```

## 问题排查：LLM 无法正确调用工具？

OpenClaw 使用 `mcporter` 来调用 MCP 工具。LLM 必须生成正确的 `mcporter call` 语法，这容易出错 — 常见问题包括遗漏 `call` 子命令、JSON 参数引号嵌套错误、或混淆服务器名称。

### 解决方案：Wrapper 脚本 + SKILL.md

创建一个简化调用语法的 wrapper 脚本，并在 SKILL.md 中描述它，让 LLM 知道如何使用。

**1. 创建 wrapper 脚本**（`~/.openclaw/workspace/tools/my-backend.sh`）：

```bash
#!/bin/bash
# 用法: my-backend.sh <工具名> <json参数>
# 示例: my-backend.sh get_weather '{"city":"Beijing"}'
mcporter call my-backend "$1" "$2"
```

```bash
chmod +x ~/.openclaw/workspace/tools/my-backend.sh
```

**2. 在 SKILL.md 中描述**（`~/.openclaw/workspace/SKILL.md`）：

```markdown
## MCP 工具 (my-backend)

使用 wrapper 脚本调用工具：

    bash ~/.openclaw/workspace/tools/my-backend.sh <工具名> '<json参数>'

可用工具：
- get_weather: 查询城市天气。参数: {"city": "string"}
- publish_post: 发布帖子（需要审批）。参数: {"title": "string", "content": "string"}
```

**3. 也添加到 TOOLS.md**（`~/.openclaw/workspace/TOOLS.md`），如果你的 Agent 从那里读取工具描述。

## 下一步

- **[策略配置指南](policy-guide_cn.md)** — 精细调整 ACL、限流、审批规则和队列设置
- **[管理 API](../README_CN.md#管理-api)** — 查询审计日志、管理待审批请求、查看限流用量
- **热更新** — 编辑 `aegis.yaml`，然后 `POST /api/v1/config/reload` — 无需重启
- **API 认证** — 在配置中设置 `server.api_token`，保护生产环境的管理端点

# 我的 AI Agent 把账号搞封了，然后我写了个 MCP 代理

上个月，我用 AI Agent 跑社交媒体自动化。Agent 接了个 MCP 工具服务器，能发帖、查数据、删内容。

我在 prompt 里写了："每天最多发 1 条，发布前先确认"。

结果呢？LLM 某次推理链里把"先确认"理解成"我已经确认过了"，30 分钟内连发了 8 条。平台直接把账号封了。

这不是个例。跑 Agent 的人或多或少都遇到过：
- prompt 说"不要超过 3 次"，Agent 第 4 次还是调了
- 说好的"只读"，结果 Agent 拼了个 publish 的参数就发出去了
- 审批流程？Agent 根本不知道审批是什么，它只知道调工具

**Prompt 是建议，不是约束。LLM 不是规则引擎。**

## 问题在哪

MCP 协议本身没有治理层。Agent 拿到 `tools/list`，看到 5 个工具，它就认为 5 个都能用。没有谁在协议层面告诉它："这个工具你不能用"、"这个工具每天只能调一次"、"这个工具调之前要人批准"。

所有的限制都靠 prompt。Prompt 是文本，LLM 是概率模型。概率模型 + 文本约束 = 偶尔会翻车。

偶尔翻车在聊天场景无所谓。但在操作真实账号的场景 —— 社交媒体、电商、客服 —— 一次翻车就够痛。

## 我的解法：在 MCP 协议层加一个代理

我写了 [Aegis MCP](https://github.com/bigmoon-dev/Aegis)，一个 MCP 协议层的治理代理。

原理很简单：Agent 不直接连 MCP 工具服务器，而是连 Aegis。Aegis 拦截所有请求，过完一套管道再决定放不放行：

```
Agent → Aegis (:18070) → MCP 工具服务器
```

管道里有什么：

1. **ACL** —— 按 Agent 粒度控制哪些工具可用。被禁的工具直接从 `tools/list` 里移除，Agent 连看都看不到
2. **限流** —— 滑动窗口，单 Agent 限流 + 跨 Agent 全局限流。共享同一个账号的多个 Agent，全局频率一起算
3. **人工审批** —— 发布、删除这类操作，请求打到 Aegis 后会挂起，通过 Webhook 通知你审批（飞书、Slack、任何 HTTP endpoint）。你不点批准，请求永远不会到后端
4. **FIFO 队列** —— 按后端串行执行，操作间随机延迟，模拟人类操作节奏
5. **审计日志** —— 每次调用全记录到 SQLite，谁调的、什么工具、什么参数、什么结果、耗时多少

关键点：**这些都是硬约束**。不管 LLM 怎么推理、Agent 怎么重试，超出限流就是 `-32002`，没审批就是挂起。Agent 绕不过去。

## 怎么用

最快体验：

```bash
npx aegis-mcp-proxy demo
```

这会启动一个 mock 工具服务器 + Aegis 代理，终端打印 curl 命令让你逐步试：
- `echo` 直接通过
- `get_weather` 调 4 次，第 4 次被限流
- `publish_post` 挂起等审批
- `admin_reset` 在 tools/list 里直接不可见

接入自己的 MCP 服务器也很简单：

```bash
npx aegis-mcp-proxy setup
```

向导会：
1. 连你的 MCP 服务器，发现所有工具
2. 根据工具名自动推荐策略（只读不限、发布加审批、危险禁用）
3. 自动检测你的 Agent（Claude Code / OpenClaw），把代理地址注入配置文件
4. 生成 `aegis.yaml` 配置文件

改配置不用重启：

```bash
curl -X POST localhost:18070/api/v1/config/reload
```

## 一些设计选择

**为什么是代理不是 SDK？**

SDK 方案要改每个 Agent 的代码。换 Agent 框架就得重新集成。代理是协议层的，Agent 只是改了个 URL，零代码改动。Claude Code、OpenClaw、自定义 Agent 都能用。

**为什么用 SQLite 不用 Redis？**

一个二进制跑起来就行，不想引入外部依赖。审计日志本来就要持久化，SQLite 刚好。整个项目只有 3 个依赖（SQLite、YAML、UUID），能跑在树莓派上。

**为什么把约束注入工具描述？**

Agent 调 `tools/list` 的时候，Aegis 会把约束信息加到工具描述里：`[Rate:1/1d|ApprovalRequired] 发布笔记`。这样 LLM 在决策时就知道这个工具有限制，能减少无谓的重试。

## 现状

- Go 编写，单二进制
- 90%+ 测试覆盖率，185 个测试
- 支持 npm / Docker / go install / GitHub Release 二进制
- Apache 2.0 开源

代码在 [github.com/bigmoon-dev/Aegis](https://github.com/bigmoon-dev/Aegis)。

如果你也在跑 AI Agent 操作真实账号，欢迎试试，也欢迎提 issue 告诉我哪里不好用。

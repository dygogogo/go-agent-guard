# go-agent-guard

MCP Guard Server — 为 AI Agent 提供预算控制、审批工作流和审计日志的 MCP Server。

适用于 Claude Code、Cursor、ChatGPT 等通过 MCP 协议交互的 AI Agent 场景。

## 功能特性

- **预算控制** — 每日 USDC 预算硬限，防止 Agent 超支
- **高风险审批** — 超过阈值或命中敏感资源的操作自动触发人工审批
- **审批工作流** — Agent 发起 → 生成 token → 人工通过 Dashboard / Telegram 审批
- **审计日志** — 所有支付、审批、拒绝操作完整记录，支持游标分页查询
- **Web Dashboard** — Gin + HTMX + Tailwind 实时仪表盘
- **Telegram 通知** — 高风险操作即时推送审批链接
- **多传输模式** — stdio / SSE / StreamableHTTP，自动检测

## MCP 工具列表

| 工具 | 说明 |
|------|------|
| `check_budget` | 查询当日预算状态 |
| `spend` | 执行消费（自动检测高风险） |
| `request_approval` | 显式请求人工审批 |
| `approve` | 批准待审批 token |
| `reject` | 拒绝待审批 token |
| `check_approval` | 轮询审批状态 |
| `get_audit_log` | 查询审计日志（支持过滤、分页） |
| `get_pending_approvals` | 列出所有待审批请求 |

## 快速开始

### 环境要求

- Go 1.24+
- 无需 CGO（使用纯 Go SQLite 驱动）

### 构建

```bash
git clone https://github.com/henrydu/mcp-guard.git
cd mcp-guard
go build -o mcp-guard main.go
```

### 配置

通过环境变量或 `.env` 文件配置：

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `MCP_TRANSPORT` | 传输模式: stdio / sse / http | 自动检测（TTY → http） |
| `BUDGET_LIMIT` | 每日预算上限（USDC） | 10.0 |
| `HIGH_RISK_THRESHOLD` | 高风险金额阈值 | 2.0 |
| `HIGH_RISK_RESOURCES` | 高风险资源关键词（逗号分隔） | delete,send |
| `DB_PATH` | SQLite 数据库路径 | ./mcp-guard.db |
| `DASHBOARD_PORT` | Dashboard 端口 | 8080 |
| `APPROVAL_BASE_URL` | 审批链接基础 URL | http://localhost:8080 |
| `LOG_LEVEL` | 日志级别: debug/info/warn/error | info |
| `PAYER_ID` | 付款方标识 | 主机名 |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token（可选） | - |
| `TELEGRAM_CHAT_ID` | Telegram Chat ID（可选） | - |

### 运行

```bash
# HTTP 模式（自动检测，带 Dashboard）
./mcp-guard
# Dashboard: http://localhost:8080/dashboard
# MCP 端点: http://localhost:8080/mcp

# stdio 模式（用于 MCP 客户端）
MCP_TRANSPORT=stdio ./mcp-guard

# SSE 模式
MCP_TRANSPORT=sse ./mcp-guard
# SSE 端点: http://localhost:8080/sse
```

### 与 Claude Code 集成

在 Claude Code 的 MCP 配置中添加：

```json
{
  "mcpServers": {
    "mcp-guard": {
      "command": "mcp-guard",
      "env": {
        "MCP_TRANSPORT": "stdio",
        "BUDGET_LIMIT": "10"
      }
    }
  }
}
```

## 架构

```
┌─────────────┐     MCP Protocol     ┌────────────────┐
│  AI Agent   │ ◄──────────────────► │  MCP Guard      │
│ (Claude等)  │   stdio / HTTP       │  Server         │
└─────────────┘                      │                  │
                                     │  ┌────────────┐ │
┌─────────────┐    HTTP              │  │  BudgetStore │ │
│  Dashboard  │ ◄──────────────────► │  │  (SQLite)    │ │
│  (Gin+HTMX) │                      │  └────────────┘ │
└─────────────┘                      └────────────────┘

┌─────────────┐    Webhook
│  Telegram   │ ◄────── 审批通知
└─────────────┘
```

### 核心文件

| 文件 | 说明 |
|------|------|
| `main.go` | 入口，传输模式选择，优雅关闭 |
| `server.go` | MCP Server，注册 8 个工具 |
| `store.go` | BudgetStore 接口 + SQLite 实现 |
| `approval.go` | 高风险检测、审批工作流、Telegram 通知 |
| `config.go` | 环境变量配置，传输自动检测 |
| `logger.go` | zap 日志（stdio 模式仅写文件） |
| `dashboard.go` | Gin Web Dashboard |

## 审批工作流

```
1. Agent 调用 spend(amount=5.0, resource="/api/delete")
2. MCP Guard 检测到高风险（金额>阈值 或 资源含关键词）
3. 返回 {status: "pending_approval", token: "xxx"}
4. Agent 调用 check_approval(token) 轮询状态
5. 人工通过 Dashboard 或 Telegram 审批/拒绝
6. Agent 收到最终结果（approved/rejected/budget_exceeded）
```

## 测试

```bash
# 运行全部测试（含 race 检测）
go test -race -count=1 ./...

# 只跑集成测试
go test -race -run TestIntegration -v ./...

# 覆盖率
go test -race -cover ./...
```

## 技术栈

- **Go 1.24** — 编程语言
- **mcp-go** — MCP 协议 Go SDK
- **Gin** — Web 框架（Dashboard）
- **modernc.org/sqlite** — 纯 Go SQLite（无需 CGO）
- **zap** — 结构化日志
- **HTMX + Tailwind CSS** — Dashboard 前端

## License

MIT

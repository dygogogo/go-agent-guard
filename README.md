# agent-guard-mcp

[![CI](https://github.com/dygogogo/agent-guard-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/dygogogo/agent-guard-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Reference](https://pkg.go.dev/badge/github.com/dygogogo/agent-guard-mcp.svg)](https://pkg.go.dev/github.com/dygogogo/agent-guard-mcp)

MCP Guard Server — Budget control, approval workflows, and audit logging for AI agents.

Works with Claude Code, Cursor, ChatGPT, and any AI agent that speaks the MCP protocol.

## Features

- **Budget Control** — Daily credits hard limit prevents agent overspending
- **High-Risk Approval** — Operations exceeding amount threshold or matching sensitive resource keywords trigger human approval
- **Approval Workflow** — Agent requests → token generated → human approves via Dashboard or Telegram
- **Audit Log** — All spend, approval, and rejection actions recorded with cursor-based pagination
- **Web Dashboard** — Gin + HTMX + Tailwind real-time dashboard
- **Telegram Notifications** — Instant approval links for high-risk operations
- **Multi-Transport** — stdio / SSE / StreamableHTTP with automatic detection

## MCP Tools

| Tool | Description |
|------|-------------|
| `check_budget` | Query today's budget status |
| `spend` | Execute a spend (auto-detects high-risk) |
| `request_approval` | Explicitly request human approval |
| `approve` | Approve a pending token |
| `reject` | Reject a pending token |
| `check_approval` | Poll approval status |
| `get_audit_log` | Query audit log with filtering and pagination |
| `get_pending_approvals` | List all pending approval requests |

## Quick Start

### Requirements

- Go 1.24+
- No CGO required (pure Go SQLite driver)

### Build from Source

```bash
git clone https://github.com/dygogogo/agent-guard-mcp.git
cd agent-guard-mcp
go build -o mcp-guard main.go
```

### Download Pre-built Binary

Download the latest release for your platform:

| Platform | amd64 | arm64 |
|----------|-------|-------|
| macOS | [darwin-amd64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) | [darwin-arm64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) |
| Linux | [linux-amd64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) | [linux-arm64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) |
| Windows | [windows-amd64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) | [windows-arm64.zip](https://github.com/dygogogo/agent-guard-mcp/releases/latest) |

Or visit the [latest release](https://github.com/dygogogo/agent-guard-mcp/releases/latest) page.

### Configuration

Configure via environment variables or `.env` file:

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_TRANSPORT` | Transport: stdio / sse / http | Auto-detect (TTY → http) |
| `BUDGET_LIMIT` | Daily budget cap (credits) | 10.0 |
| `HIGH_RISK_THRESHOLD` | High-risk amount threshold | 2.0 |
| `HIGH_RISK_RESOURCES` | High-risk resource keywords (comma-separated) | delete,send |
| `DB_PATH` | SQLite database path | ./mcp-guard.db |
| `DASHBOARD_PORT` | Dashboard HTTP port | 8080 |
| `APPROVAL_BASE_URL` | Base URL for approval links | http://localhost:8080 |
| `LOG_LEVEL` | Log level: debug/info/warn/error | info |
| `PAYER_ID` | Payer identity | hostname |
| `TELEGRAM_BOT_TOKEN` | Telegram Bot Token (optional) | - |
| `TELEGRAM_CHAT_ID` | Telegram Chat ID (optional) | - |

### Running

```bash
# HTTP mode (auto-detected, with Dashboard)
./mcp-guard
# Dashboard: http://localhost:8080/dashboard
# MCP endpoint: http://localhost:8080/mcp

# stdio mode (for MCP clients)
MCP_TRANSPORT=stdio ./mcp-guard

# SSE mode
MCP_TRANSPORT=sse ./mcp-guard
# SSE endpoint: http://localhost:8080/sse
```

### Claude Code Integration

Add to Claude Code's MCP configuration:

```json
{
  "mcpServers": {
    "agent-guard-mcp": {
      "command": "mcp-guard",
      "env": {
        "MCP_TRANSPORT": "stdio",
        "BUDGET_LIMIT": "10"
      }
    }
  }
}
```

## Architecture

```
┌─────────────┐     MCP Protocol     ┌────────────────┐
│  AI Agent   │ ◄──────────────────► │  MCP Guard      │
│ (Claude,    │   stdio / HTTP       │  Server         │
│  Cursor...) │                      │                  │
└─────────────┘                      │  ┌────────────┐ │
                                     │  │  BudgetStore │ │
┌─────────────┐    HTTP              │  │  (SQLite)    │ │
│  Dashboard  │ ◄──────────────────► │  └────────────┘ │
│  (Gin+HTMX) │                      └────────────────┘
└─────────────┘

┌─────────────┐    Webhook
│  Telegram   │ ◄────── Approval notifications
└─────────────┘
```

### Core Files

| File | Description |
|------|-------------|
| `main.go` | Entry point, transport selection, graceful shutdown |
| `server.go` | MCP Server with 8 registered tools |
| `store.go` | BudgetStore interface + SQLite implementation |
| `approval.go` | High-risk detection, approval workflow, Telegram |
| `config.go` | Environment config, auto transport detection |
| `logger.go` | zap logging (stdio mode: file only) |
| `dashboard.go` | Gin Web Dashboard |

## Approval Workflow

```
1. Agent calls spend(amount=5.0, resource="/api/delete")
2. MCP Guard detects high-risk (amount > threshold OR resource keyword matched)
3. Returns {status: "pending_approval", token: "xxx"}
4. Agent polls check_approval(token) for status
5. Human approves/rejects via Dashboard or Telegram
6. Agent receives final result (approved/rejected/budget_exceeded)
```

## Testing

```bash
# All tests with race detection
go test -race -count=1 ./...

# Integration tests only
go test -race -run TestIntegration -v ./...

# Coverage
go test -race -cover ./...
```

## Tech Stack

- **Go 1.24** — Language
- **mcp-go** — MCP protocol Go SDK
- **Gin** — Web framework (Dashboard)
- **modernc.org/sqlite** — Pure Go SQLite (no CGO)
- **zap** — Structured logging
- **HTMX + Tailwind CSS** — Dashboard frontend

## License

MIT

---

[中文](./README_zh.md)

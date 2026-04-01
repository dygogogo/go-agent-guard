package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

// NewMCPGuardServer creates the MCP server with all tools registered.
func NewMCPGuardServer(store BudgetStore, cfg *Config, logger *zap.Logger) *server.MCPServer {
	s := server.NewMCPServer("mcp-guard", "1.0.0",
		server.WithToolCapabilities(false),
	)

	registerTools(s, store, cfg, logger)
	return s
}

func registerTools(s *server.MCPServer, store BudgetStore, cfg *Config, logger *zap.Logger) {
	s.AddTool(mcp.NewTool("check_budget",
		mcp.WithDescription("Query current daily budget status"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleCheckBudget(store, cfg)
	})

	s.AddTool(mcp.NewTool("spend",
		mcp.WithDescription("Execute a spending action. Checks budget, detects high-risk, records transaction."),
		mcp.WithNumber("amount", mcp.Required(), mcp.Description("Amount to spend in USDC")),
		mcp.WithString("resource", mcp.Required(), mcp.Description("Target resource identifier")),
		mcp.WithString("description", mcp.Description("Optional description of the spend")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleSpend(store, cfg, logger, req)
	})

	s.AddTool(mcp.NewTool("request_approval",
		mcp.WithDescription("Explicitly request human approval for an action"),
		mcp.WithNumber("amount", mcp.Required(), mcp.Description("Amount in USDC")),
		mcp.WithString("resource", mcp.Required(), mcp.Description("Target resource")),
		mcp.WithString("reason", mcp.Required(), mcp.Description("Reason for approval request")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleRequestApproval(store, cfg, logger, req)
	})

	s.AddTool(mcp.NewTool("approve",
		mcp.WithDescription("Approve a pending approval request"),
		mcp.WithString("token", mcp.Required(), mcp.Description("Approval token to approve")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleApprove(store, cfg, logger, req)
	})

	s.AddTool(mcp.NewTool("reject",
		mcp.WithDescription("Reject a pending approval request"),
		mcp.WithString("token", mcp.Required(), mcp.Description("Approval token to reject")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleReject(store, cfg, logger, req)
	})

	s.AddTool(mcp.NewTool("check_approval",
		mcp.WithDescription("Poll the status of a pending approval. Call every 15-30 seconds until resolved or expired."),
		mcp.WithString("token", mcp.Required(), mcp.Description("Approval token to check")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleCheckApproval(store, cfg, req)
	})

	s.AddTool(mcp.NewTool("get_audit_log",
		mcp.WithDescription("Query audit history with optional filtering and cursor-based pagination"),
		mcp.WithNumber("limit", mcp.Description("Max entries to return (default 20, max 100)")),
		mcp.WithString("action_filter", mcp.Description("Filter by action type")),
		mcp.WithString("cursor", mcp.Description("Pagination cursor from previous response")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleGetAuditLog(store, req)
	})

	s.AddTool(mcp.NewTool("get_pending_approvals",
		mcp.WithDescription("List all pending approval requests"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return handleGetPendingApprovals(store)
	})
}

func handleCheckBudget(store BudgetStore, cfg *Config) (*mcp.CallToolResult, error) {
	spent, count, err := store.GetDailySpent()
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	return toolResult(map[string]interface{}{
		"limit":         cfg.BudgetLimit,
		"spent":         spent,
		"remaining":     cfg.BudgetLimit - spent,
		"request_count": count,
		"date":          fmtTimeDate(),
	})
}

func handleSpend(store BudgetStore, cfg *Config, logger *zap.Logger, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	amount, err := req.RequireFloat("amount")
	if err != nil {
		return toolError("invalid_amount", "amount is required and must be a number")
	}
	resource, err := req.RequireString("resource")
	if err != nil {
		return toolError("invalid_resource", "resource is required")
	}

	payer := cfg.PayerID

	if IsHighRisk(amount, resource, cfg.HighRiskThreshold, cfg.HighRiskResources) {
		token, err := CreateApproval(store, cfg, logger, payer, amount, resource)
		if err != nil {
			return toolError("internal_error", err.Error())
		}
		return toolResult(map[string]interface{}{
			"status":     "pending_approval",
			"token":      token,
			"expires_in": 1800,
		})
	}

	ok, reason, newTotal, _, txID, spendErr := store.CheckAndSpend(payer, amount, cfg.BudgetLimit, resource)
	if spendErr != nil {
		return toolError("internal_error", spendErr.Error())
	}
	if !ok {
		_ = store.InsertAuditLog("budget_exceeded", amount, "rejected", reason, payer, resource, "")
		logger.Warn("budget exceeded", zap.Float64("amount", amount), zap.String("reason", reason))
		return toolResult(map[string]interface{}{
			"ok":            false,
			"error":         "budget_exceeded",
			"message":       reason,
			"current_spent": newTotal,
		})
	}

	_ = store.InsertAuditLog("payment_success", amount, "success", "spend executed", payer, resource, txID)
	logger.Info("spend success", zap.Float64("amount", amount), zap.String("txID", txID))
	return toolResult(map[string]interface{}{
		"ok":             true,
		"remaining":      cfg.BudgetLimit - newTotal,
		"transaction_id": txID,
	})
}

func handleRequestApproval(store BudgetStore, cfg *Config, logger *zap.Logger, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	amount, err := req.RequireFloat("amount")
	if err != nil {
		return toolError("invalid_amount", "amount is required")
	}
	resource, err := req.RequireString("resource")
	if err != nil {
		return toolError("invalid_resource", "resource is required")
	}
	_, err = req.RequireString("reason")
	if err != nil {
		return toolError("invalid_reason", "reason is required")
	}

	payer := cfg.PayerID
	token, err := CreateApproval(store, cfg, logger, payer, amount, resource)
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	return toolResult(map[string]interface{}{
		"token":      token,
		"expires_in": 1800,
		"status":     "pending",
	})
}

func handleApprove(store BudgetStore, cfg *Config, logger *zap.Logger, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	token, err := req.RequireString("token")
	if err != nil {
		return toolError("invalid_token", "token is required")
	}
	result, err := ResolveApproval(store, cfg, logger, token, true)
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	return toolResult(result)
}

func handleReject(store BudgetStore, cfg *Config, logger *zap.Logger, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	token, err := req.RequireString("token")
	if err != nil {
		return toolError("invalid_token", "token is required")
	}
	result, err := ResolveApproval(store, cfg, logger, token, false)
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	return toolResult(result)
}

func handleCheckApproval(store BudgetStore, cfg *Config, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	token, err := req.RequireString("token")
	if err != nil {
		return toolError("invalid_token", "token is required")
	}
	status, payer, amount, _, err := store.GetApprovalStatus(token)
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	if status == "" {
		return toolError("not_found", "token not found")
	}

	result := map[string]interface{}{
		"status": status,
		"payer":  payer,
		"amount": amount,
	}
	if status == "approved" {
		result["remaining"] = cfg.BudgetLimit - amount
	}
	return toolResult(result)
}

func handleGetAuditLog(store BudgetStore, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limit := int(req.GetFloat("limit", 20))
	actionFilter := req.GetString("action_filter", "")
	cursor := req.GetString("cursor", "")

	entries, nextCursor, err := store.GetAuditLogs(limit, actionFilter, cursor)
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	return toolResult(map[string]interface{}{
		"entries":     entries,
		"next_cursor": nextCursor,
	})
}

func handleGetPendingApprovals(store BudgetStore) (*mcp.CallToolResult, error) {
	approvals, err := store.GetPendingApprovals()
	if err != nil {
		return toolError("internal_error", err.Error())
	}
	if approvals == nil {
		approvals = []PendingApproval{}
	}
	return toolResult(approvals)
}

func toolResult(data interface{}) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func toolError(code string, message string) (*mcp.CallToolResult, error) {
	data := map[string]string{"ok": "false", "error": code, "message": message}
	return toolResult(data)
}

func fmtTimeDate() string {
	return currentTime().Format("2006-01-02")
}

func currentTime() time.Time {
	return time.Now()
}

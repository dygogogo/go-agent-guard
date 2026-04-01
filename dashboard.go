package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// NewDashboardRouter creates a Gin engine serving the dashboard.
func NewDashboardRouter(store BudgetStore, cfg *Config, logger *zap.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/dashboard", DashboardHandler(store, cfg, logger))
	r.GET("/dashboard/approvals", DashboardApprovalsHandler(store))
	r.GET("/dashboard/logs", DashboardLogsHandler(store))
	r.GET("/approve/:token", HandleApprovalAction(store, cfg, logger, true))
	r.GET("/reject/:token", HandleApprovalAction(store, cfg, logger, false))
	return r
}

// DashboardHandler renders the main dashboard page.
func DashboardHandler(store BudgetStore, cfg *Config, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		spent, count, err := store.GetDailySpent()
		if err != nil {
			c.String(http.StatusInternalServerError, "Error: %v", err)
			return
		}
		_ = count

		logs, _, err := store.GetAuditLogs(10, "", "")
		if err != nil {
			logs = []AuditLogEntry{}
		}

		approvals, err := store.GetPendingApprovals()
		if err != nil {
			approvals = []PendingApproval{}
		}

		remaining := cfg.BudgetLimit - spent

		data := map[string]interface{}{
			"BudgetLimit":      fmt.Sprintf("%.2f", cfg.BudgetLimit),
			"CurrentSpent":     fmt.Sprintf("%.2f", spent),
			"Remaining":        fmt.Sprintf("%.2f", remaining),
			"AuditLogs":        logs,
			"PendingApprovals": approvals,
		}

		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, renderTemplate(dashboardHTML, data))
	}
}

// DashboardApprovalsHandler renders the HTMX partial for approvals list.
func DashboardApprovalsHandler(store BudgetStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		approvals, err := store.GetPendingApprovals()
		if err != nil {
			c.String(http.StatusInternalServerError, "Error: %v", err)
			return
		}

		if len(approvals) == 0 {
			c.String(http.StatusOK, "<p class=\"text-gray-500 text-center py-4\">No pending approval requests</p>")
			return
		}

		var html string
		for _, a := range approvals {
			html += fmt.Sprintf(`<tr class="border-t">
				<td class="py-2">%s</td>
				<td class="py-2 font-mono">$%.2f</td>
				<td class="py-2 text-sm">%s</td>
				<td class="py-2">
					<a href="/approve/%s" class="text-green-600 hover:text-green-800 font-medium mr-2">Approve</a>
					<a href="/reject/%s" class="text-red-600 hover:text-red-800 font-medium">Reject</a>
				</td>
			</tr>`, a.Payer, a.Amount, a.Resource, a.Token, a.Token)
		}
		c.String(http.StatusOK, html)
	}
}

// DashboardLogsHandler renders the HTMX partial for audit logs.
func DashboardLogsHandler(store BudgetStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		logs, _, err := store.GetAuditLogs(10, "", "")
		if err != nil {
			logs = []AuditLogEntry{}
		}

		var html string
		for _, log := range logs {
			amount := "-"
			if log.Amount > 0 {
				amount = fmt.Sprintf("$%.2f", log.Amount)
			}
			html += fmt.Sprintf(`<tr class="border-t">
				<td class="py-2 text-sm text-gray-500">%s</td>
				<td class="py-2">%s</td>
				<td class="py-2 font-mono">%s</td>
				<td class="py-2">
					<span class="px-2 py-1 rounded text-xs font-medium status-%s">%s</span>
				</td>
			</tr>`, log.Timestamp, log.Action, amount, log.Status, log.Status)
		}
		c.String(http.StatusOK, html)
	}
}

// HandleApprovalAction handles approve/reject from dashboard.
func HandleApprovalAction(store BudgetStore, cfg *Config, logger *zap.Logger, approved bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Param("token")

		status, _, _, _, err := store.GetApprovalStatus(token)
		if err != nil {
			c.String(http.StatusInternalServerError, "Error: %v", err)
			return
		}
		if status == "" {
			c.String(http.StatusNotFound, "Token not found")
			return
		}
		if status != "pending" {
			msg := fmt.Sprintf("Token already %s", status)
			c.String(http.StatusBadRequest, msg)
			return
		}

		if approved {
			result, err := ResolveApproval(store, cfg, logger, token, true)
			if err != nil {
				c.String(http.StatusInternalServerError, "Error: %v", err)
				return
			}
			c.Header("Content-Type", "text/html")
			c.String(http.StatusOK, fmt.Sprintf(`<div class="bg-green-50 border-t border-green-200 p-3">
				<p class="font-bold">Approved!</p>
				<p>Payer: %s</p>
				<p>Amount: $%.2f USDC</p>
				<p>Resource: %s</p>
				<p><a href="/dashboard" class="text-blue-600 hover:underline">Back to Dashboard</a></p>
			</div>`, result.Payer, result.Amount, result.Resource))
		} else {
			result, err := ResolveApproval(store, cfg, logger, token, false)
			if err != nil {
				c.String(http.StatusInternalServerError, "Error: %v", err)
				return
			}
			c.Header("Content-Type", "text/html")
			c.String(http.StatusOK, fmt.Sprintf(`<div class="bg-red-50 border-t border-red-200 p-3">
				<p class="font-bold">Rejected</p>
				<p>Payer: %s</p>
				<p>Amount: $%.2f USDC</p>
				<p>Resource: %s</p>
				<p><a href="/dashboard" class="text-blue-600 hover:underline">Back to Dashboard</a></p>
			</div>`, result.Payer, result.Amount, result.Resource))
		}
	}
}

func renderTemplate(tmpl string, data map[string]interface{}) string {
	result := tmpl
	for k, v := range data {
		result = strings.ReplaceAll(result, "{{."+k+"}}", fmt.Sprintf("%v", v))
	}
	return result
}

// TODO: replace with proper html/template in production
var dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MCP Guard Dashboard</title>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-gray-50">
    <div class="min-h-screen">
        <nav class="bg-white shadow">
            <div class="max-w-7xl mx-auto px-4 py-4">
                <h1 class="text-xl font-bold text-gray-800">MCP Guard Dashboard</h1>
            </div>
        </nav>

        <main class="max-w-7xl mx-auto px-4 py-6">
            <div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
                <div class="bg-white rounded-lg shadow p-6">
                    <p class="text-sm text-gray-500">Daily Budget</p>
                    <p class="text-2xl font-bold text-gray-900">${{.BudgetLimit}}</p>
                </div>
                <div class="bg-white rounded-lg shadow p-6">
                    <p class="text-sm text-gray-500">Spent Today</p>
                    <p class="text-2xl font-bold text-red-600">${{.CurrentSpent}}</p>
                </div>
                <div class="bg-white rounded-lg shadow p-6">
                    <p class="text-sm text-gray-500">Remaining</p>
                    <p class="text-2xl font-bold text-green-600">${{.Remaining}}</p>
                </div>
            </div>

            <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
                <div class="bg-white rounded-lg shadow">
                    <div class="px-6 py-4 border-b">
                        <h2 class="text-lg font-semibold">Pending Approvals</h2>
                    </div>
                    <div class="p-4" hx-get="/dashboard/approvals" hx-trigger="every 5s">
                        <table class="min-w-full">
                            <thead>
                                <tr class="text-left text-xs font-medium text-gray-500 uppercase">
                                    <th class="pb-2">Payer</th>
                                    <th class="pb-2">Amount</th>
                                    <th class="pb-2">Resource</th>
                                    <th class="pb-2">Actions</th>
                                </tr>
                            </thead>
                            <tbody id="approvals-body">
                            </tbody>
                        </table>
                    </div>
                </div>

                <div class="bg-white rounded-lg shadow">
                    <div class="px-6 py-4 border-b flex justify-between items-center">
                        <h2 class="text-lg font-semibold">Audit Log</h2>
                        <button hx-get="/dashboard/logs" hx-target="#audit-logs" class="text-sm text-blue-600 hover:underline">Refresh</button>
                    </div>
                    <div class="p-4" id="audit-logs">
                        <table class="min-w-full">
                            <thead>
                                <tr class="text-left text-xs font-medium text-gray-500 uppercase">
                                    <th class="pb-2">Time</th>
                                    <th class="pb-2">Action</th>
                                    <th class="pb-2">Amount</th>
                                    <th class="pb-2">Status</th>
                                </tr>
                            </thead>
                            <tbody>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        </main>
    </div>
</body>
</html>`

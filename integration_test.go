package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

func newIntegrationEnv(t *testing.T) (BudgetStore, *Config, *zap.Logger) {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &Config{
		BudgetLimit:       10.0,
		HighRiskThreshold: 2.0,
		HighRiskResources: []string{"delete", "send"},
		PayerID:           "test-agent",
	}
	return store, cfg, zap.NewNop()
}

func parseResultText(result *mcp.CallToolResult) map[string]interface{} {
	t := result.Content[0].(mcp.TextContent).Text
	var data map[string]interface{}
	json.Unmarshal([]byte(t), &data)
	return data
}

func TestIntegration_CheckBudget_Empty(t *testing.T) {
	store, cfg, _ := newIntegrationEnv(t)
	result, err := handleCheckBudget(store, cfg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	data := parseResultText(result)
	if data["limit"] != 10.0 {
		t.Errorf("limit = %v, want 10.0", data["limit"])
	}
	if data["remaining"] != 10.0 {
		t.Errorf("remaining = %v, want 10.0", data["remaining"])
	}
	if data["spent"] != 0.0 {
		t.Errorf("spent = %v, want 0", data["spent"])
	}
}

func TestIntegration_Spend_Success(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   1.5,
			"resource": "/api/query",
		},
	}
	result, err := handleSpend(store, cfg, logger, req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	data := parseResultText(result)
	if data["ok"] != true {
		t.Errorf("ok = %v, want true", data["ok"])
	}
	if data["remaining"] != 8.5 {
		t.Errorf("remaining = %v, want 8.5", data["remaining"])
	}
	if data["transaction_id"] == nil {
		t.Error("expected transaction_id")
	}
}

func TestIntegration_Spend_BudgetExceeded(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	// Spend 1.5 six times = 9.0 total
	for i := 0; i < 6; i++ {
		req := mcp.CallToolRequest{}
		req.Params = mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"amount":   1.5,
				"resource": "/api/query",
			},
		}
		handleSpend(store, cfg, logger, req)
	}
	// 7th spend should exceed
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   2.0,
			"resource": "/api/query",
		},
	}
	result, _ := handleSpend(store, cfg, logger, req)
	data := parseResultText(result)
	if data["ok"] != false {
		t.Errorf("ok = %v, want false (budget exceeded)", data["ok"])
	}
	if data["error"] != "budget_exceeded" {
		t.Errorf("error = %v, want budget_exceeded", data["error"])
	}
}

func TestIntegration_Spend_HighRiskAmount(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   5.0,
			"resource": "/api/query",
		},
	}
	result, _ := handleSpend(store, cfg, logger, req)
	data := parseResultText(result)
	if data["status"] != "pending_approval" {
		t.Errorf("status = %v, want pending_approval", data["status"])
	}
}

func TestIntegration_Spend_HighRiskResource(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   0.5,
			"resource": "/api/delete/resource",
		},
	}
	result, _ := handleSpend(store, cfg, logger, req)
	data := parseResultText(result)
	if data["status"] != "pending_approval" {
		t.Errorf("status = %v, want pending_approval", data["status"])
	}
}

func TestIntegration_ApprovalWorkflow_ApproveCycle(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	// Create approval
	token, err := CreateApproval(store, cfg, logger, "test-agent", 5.0, "/api/delete")
	if err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	// Check pending
	checkReq := mcp.CallToolRequest{}
	checkReq.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{"token": token},
	}
	checkResult, _ := handleCheckApproval(store, cfg, checkReq)
	checkData := parseResultText(checkResult)
	if checkData["status"] != "pending" {
		t.Errorf("status = %v, want pending", checkData["status"])
	}
	// Approve
	approveReq := mcp.CallToolRequest{}
	approveReq.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{"token": token},
	}
	approveResult, err := handleApprove(store, cfg, logger, approveReq)
	if err != nil {
		t.Fatalf("handleApprove: %v", err)
	}
	approveData := parseResultText(approveResult)
	if approveData["status"] != "approved" {
		t.Errorf("status = %v, want approved", approveData["status"])
	}
	// Verify budget deducted
	budgetResult, _ := handleCheckBudget(store, cfg)
	budgetData := parseResultText(budgetResult)
	if budgetData["spent"] != 5.0 {
		t.Errorf("spent = %v, want 5.0", budgetData["spent"])
	}
}

func TestIntegration_ApprovalWorkflow_RejectCycle(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 5.0, "/api/delete")
	rejectReq := mcp.CallToolRequest{}
	rejectReq.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{"token": token},
	}
	rejectResult, _ := handleReject(store, cfg, logger, rejectReq)
	rejectData := parseResultText(rejectResult)
	if rejectData["status"] != "rejected" {
		t.Errorf("status = %v, want rejected", rejectData["status"])
	}
	// Verify budget not deducted
	budgetResult, _ := handleCheckBudget(store, cfg)
	budgetData := parseResultText(budgetResult)
	if budgetData["spent"] != 0.0 {
		t.Errorf("spent = %v, want 0 (rejected)", budgetData["spent"])
	}
}

func TestIntegration_AuditLog(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   1.0,
			"resource": "/api/query",
		},
	}
	handleSpend(store, cfg, logger, req)
	auditReq := mcp.CallToolRequest{}
	auditReq.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{"limit": float64(10)},
	}
	result, _ := handleGetAuditLog(store, auditReq)
	data := parseResultText(result)
	entries, ok := data["entries"].([]interface{})
	if !ok || len(entries) == 0 {
		t.Error("expected audit log entries")
	}
}

func TestIntegration_PendingApprovals(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	CreateApproval(store, cfg, logger, "test-agent", 5.0, "/api/delete")
	result, _ := handleGetPendingApprovals(store)
	text := result.Content[0].(mcp.TextContent).Text
	if !strings.Contains(text, "test-agent") {
		t.Error("pending approvals should contain payer")
	}
}

// --- Dashboard Integration Tests ---

func TestIntegration_DashboardApproveReject(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	router := NewDashboardRouter(store, cfg, logger)

	// Main page
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/dashboard", nil)
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("dashboard status = %d, want 200", w.Code)
	}

	// Create approval and approve
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 5.0, "/api/delete")

	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Errorf("approve status = %d, want 200", w2.Code)
	}
	if !strings.Contains(w2.Body.String(), "Approved") {
		t.Error("should say Approved")
	}

	// Reject another
	token2, _ := CreateApproval(store, cfg, logger, "test-agent", 3.0, "/api/delete")
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("GET", "/reject/"+token2, nil)
	router.ServeHTTP(w3, r3)
	if w3.Code != 200 {
		t.Errorf("reject status = %d, want 200", w3.Code)
	}
	if !strings.Contains(w3.Body.String(), "Rejected") {
		t.Error("should say Rejected")
	}
}

func TestIntegration_DashboardTokenNotFound(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/approve/nonexistent", nil)
	router.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIntegration_DashboardAlreadyResolved(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	router := NewDashboardRouter(store, cfg, logger)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 5.0, "/api/delete")
	// Approve
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w1, r1)
	// Approve again
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w2, r2)
	if w2.Code != 400 {
		t.Errorf("status = %d, want 400", w2.Code)
	}
}

func TestIntegration_MCPDashboardConsistency(t *testing.T) {
	store, cfg, logger := newIntegrationEnv(t)
	// Spend via MCP → triggers approval
	req := mcp.CallToolRequest{}
	req.Params = mcp.CallToolParams{
		Arguments: map[string]interface{}{
			"amount":   3.0,
			"resource": "/api/delete",
		},
	}
	result, _ := handleSpend(store, cfg, logger, req)
	data := parseResultText(result)
	token, ok := data["token"].(string)
	if !ok || token == "" {
		t.Fatal("expected token")
	}
	// Approve via dashboard
	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// Verify via MCP check_budget
	budgetResult, _ := handleCheckBudget(store, cfg)
	budgetData := parseResultText(budgetResult)
	if budgetData["spent"] != 3.0 {
		t.Errorf("spent = %v, want 3.0", budgetData["spent"])
	}
}

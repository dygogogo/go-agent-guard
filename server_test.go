package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

func setupTestEnv(t *testing.T) (BudgetStore, *Config, *zap.Logger) {
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
	logger := zap.NewNop()
	return store, cfg, logger
}

func newReq(args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content in result")
	return ""
}

func parseMap(t *testing.T, result *mcp.CallToolResult) map[string]interface{} {
	t.Helper()
	text := extractText(t, result)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	return m
}

// --- handleCheckBudget ---

func TestHandleCheckBudget(t *testing.T) {
	store, cfg, _ := setupTestEnv(t)
	result, err := handleCheckBudget(store, cfg)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["limit"] != 10.0 {
		t.Errorf("limit = %v, want 10.0", resp["limit"])
	}
	if resp["remaining"] != 10.0 {
		t.Errorf("remaining = %v, want 10.0", resp["remaining"])
	}
}

func TestHandleCheckBudget_AfterSpend(t *testing.T) {
	store, cfg, _ := setupTestEnv(t)
	ok, _, _, _, _, err := store.CheckAndSpend("test-agent", 3.0, cfg.BudgetLimit, "/api/call")
	if err != nil || !ok {
		t.Fatalf("CheckAndSpend failed")
	}
	result, _ := handleCheckBudget(store, cfg)
	resp := parseMap(t, result)
	if resp["spent"] != 3.0 {
		t.Errorf("spent = %v, want 3.0", resp["spent"])
	}
}

// --- handleSpend ---

func TestHandleSpend_Success(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	result, err := handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.5, "resource": "/api/call",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["ok"] != true {
		t.Errorf("ok = %v, want true", resp["ok"])
	}
	if resp["remaining"] != 8.5 {
		t.Errorf("remaining = %v, want 8.5", resp["remaining"])
	}
}

func TestHandleSpend_BudgetExceeded(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	// Spend 9.0 in small amounts below the high-risk threshold (2.0)
	handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.8, "resource": "/api/call",
	}))
	handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.8, "resource": "/api/call",
	}))
	handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.8, "resource": "/api/call",
	}))
	handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.8, "resource": "/api/call",
	}))
	handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 1.8, "resource": "/api/call",
	}))
	result, err := handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 2.0, "resource": "/api/call",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["ok"] != false {
		t.Errorf("ok = %v, want false", resp["ok"])
	}
	if resp["error"] != "budget_exceeded" {
		t.Errorf("error = %v, want budget_exceeded", resp["error"])
	}
}

func TestHandleSpend_HighRiskAmount(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	result, err := handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 5.0, "resource": "/api/call",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "pending_approval" {
		t.Errorf("status = %v, want pending_approval", resp["status"])
	}
	if resp["token"] == "" {
		t.Error("token should not be empty")
	}
}

func TestHandleSpend_HighRiskResource(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	result, err := handleSpend(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 0.5, "resource": "/api/delete/user",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "pending_approval" {
		t.Errorf("should trigger approval for delete resource, got status=%v", resp["status"])
	}
}

// --- handleRequestApproval ---

func TestHandleRequestApproval(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	result, err := handleRequestApproval(store, cfg, logger, newReq(map[string]interface{}{
		"amount": 3.0, "resource": "/api/action", "reason": "need review",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
	if resp["token"] == "" {
		t.Error("token should not be empty")
	}
}

// --- handleApprove / handleReject ---

func TestHandleApprove(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 1.0, "/api/data")
	result, err := handleApprove(store, cfg, logger, newReq(map[string]interface{}{
		"token": token,
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "approved" {
		t.Errorf("status = %v, want approved", resp["status"])
	}
}

func TestHandleReject(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 1.0, "/api/data")
	result, err := handleReject(store, cfg, logger, newReq(map[string]interface{}{
		"token": token,
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "rejected" {
		t.Errorf("status = %v, want rejected", resp["status"])
	}
}

func TestHandleApprove_MissingToken(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	result, err := handleApprove(store, cfg, logger, newReq(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %v, want invalid_token", resp["error"])
	}
}

// --- handleCheckApproval ---

func TestHandleCheckApproval_Pending(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 3.0, "/api/data")
	result, err := handleCheckApproval(store, cfg, newReq(map[string]interface{}{
		"token": token,
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
}

func TestHandleCheckApproval_Approved(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "test-agent", 1.0, "/api/data")
	ResolveApproval(store, cfg, logger, token, true)

	result, err := handleCheckApproval(store, cfg, newReq(map[string]interface{}{
		"token": token,
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["status"] != "approved" {
		t.Errorf("status = %v, want approved", resp["status"])
	}
}

func TestHandleCheckApproval_NotFound(t *testing.T) {
	store, cfg, _ := setupTestEnv(t)
	result, err := handleCheckApproval(store, cfg, newReq(map[string]interface{}{
		"token": "nonexistent",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["error"] != "not_found" {
		t.Errorf("error = %v, want not_found", resp["error"])
	}
}

func TestHandleCheckApproval_MissingToken(t *testing.T) {
	store, cfg, _ := setupTestEnv(t)
	result, err := handleCheckApproval(store, cfg, newReq(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["error"] != "invalid_token" {
		t.Errorf("error = %v, want invalid_token", resp["error"])
	}
}

// --- handleGetAuditLog ---

func TestHandleGetAuditLog(t *testing.T) {
	store, _, _ := setupTestEnv(t)
	_ = store.InsertAuditLog("payment_success", 1.5, "success", "test", "payer1", "/api/call", "tx-1")
	_ = store.InsertAuditLog("approval_pending", 5.0, "pending", "high risk", "payer1", "/api/delete", "tok-1")

	result, err := handleGetAuditLog(store, newReq(map[string]interface{}{}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	entries, ok := resp["entries"].([]interface{})
	if !ok || len(entries) != 2 {
		t.Errorf("entries = %v, want 2", resp["entries"])
	}
}

func TestHandleGetAuditLog_WithFilter(t *testing.T) {
	store, _, _ := setupTestEnv(t)
	_ = store.InsertAuditLog("payment_success", 1.5, "success", "test", "p1", "/api", "tx-1")
	_ = store.InsertAuditLog("approval_pending", 5.0, "pending", "test", "p1", "/api", "tok-1")

	result, err := handleGetAuditLog(store, newReq(map[string]interface{}{
		"action_filter": "payment_success",
	}))
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	entries, ok := resp["entries"].([]interface{})
	if !ok || len(entries) != 1 {
		t.Errorf("entries = %v, want 1 filtered entry", resp["entries"])
	}
}

// --- handleGetPendingApprovals ---

func TestHandleGetPendingApprovals(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	CreateApproval(store, cfg, logger, "payer1", 3.0, "/api/delete")
	CreateApproval(store, cfg, logger, "payer2", 4.0, "/api/send")

	result, err := handleGetPendingApprovals(store)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := extractText(t, result)
	var approvals []interface{}
	if err := json.Unmarshal([]byte(text), &approvals); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(approvals) != 2 {
		t.Errorf("expected 2 approvals, got %d", len(approvals))
	}
}

func TestHandleGetPendingApprovals_Empty(t *testing.T) {
	store, _, _ := setupTestEnv(t)
	result, err := handleGetPendingApprovals(store)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	text := extractText(t, result)
	var approvals []interface{}
	if err := json.Unmarshal([]byte(text), &approvals); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(approvals) != 0 {
		t.Errorf("expected 0 approvals, got %d", len(approvals))
	}
}

// --- toolError ---

func TestToolError(t *testing.T) {
	result, err := toolError("test_code", "test message")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	resp := parseMap(t, result)
	if resp["error"] != "test_code" {
		t.Errorf("error = %v, want test_code", resp["error"])
	}
	if resp["message"] != "test message" {
		t.Errorf("message = %v, want 'test message'", resp["message"])
	}
}

// --- Dashboard HTTP tests ---

func TestDashboardHTTP_MainPage(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	router := NewDashboardRouter(store, cfg, logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"MCP Guard", "Budget", "Approvals"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestDashboardHTTP_ApprovalsEmpty(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	router := NewDashboardRouter(store, cfg, logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard/approvals", nil)
	router.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "No pending") {
		t.Error("should show empty message")
	}
}

func TestDashboardHTTP_ApprovalsData(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	CreateApproval(store, cfg, logger, "payer1", 5.0, "/api/delete")

	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard/approvals", nil)
	router.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "Approve") {
		t.Error("should contain approve link")
	}
}

func TestDashboardHTTP_LogsPartial(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	_ = store.InsertAuditLog("payment_success", 1.5, "success", "test", "payer1", "/api/call", "tx-1")

	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard/logs", nil)
	router.ServeHTTP(w, req)

	if !strings.Contains(w.Body.String(), "payment_success") {
		t.Error("should contain action")
	}
}

func TestDashboardHTTP_ApproveAction(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "payer1", 1.0, "/api/data")

	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Approved") {
		t.Error("should show approved")
	}
}

func TestDashboardHTTP_RejectAction(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "payer1", 1.0, "/api/data")

	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/reject/"+token, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Rejected") {
		t.Error("should show rejected")
	}
}

func TestDashboardHTTP_ApproveNotFound(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	router := NewDashboardRouter(store, cfg, logger)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/approve/nonexistent", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDashboardHTTP_ApproveAlreadyResolved(t *testing.T) {
	store, cfg, logger := setupTestEnv(t)
	token, _ := CreateApproval(store, cfg, logger, "payer1", 1.0, "/api/data")
	ResolveApproval(store, cfg, logger, token, true)

	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// --- NewMCPGuardServer ---

func TestNewMCPGuardServer(t *testing.T) {
	store, cfg, _ := setupTestEnv(t)
	s := NewMCPGuardServer(store, cfg, zap.NewNop())
	if s == nil {
		t.Fatal("NewMCPGuardServer returned nil")
	}
}

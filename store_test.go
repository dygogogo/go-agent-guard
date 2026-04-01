package main

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore() error: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestNewSQLiteStore_CreatesSchema(t *testing.T) {
	store := newTestStore(t)

	// Verify tables exist by querying them
	tables := []string{"daily_budget", "spending_log", "audit_log", "approval_token"}
	for _, table := range tables {
		var name string
		err := store.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestCheckAndSpend_Success(t *testing.T) {
	store := newTestStore(t)

	ok, reason, newTotal, count, txID, err := store.CheckAndSpend("payer1", 0.5, 10.0, "/api/query")
	if err != nil {
		t.Fatalf("CheckAndSpend() error: %v", err)
	}
	if !ok {
		t.Errorf("CheckAndSpend() ok = false, reason: %s", reason)
	}
	if newTotal != 0.5 {
		t.Errorf("newTotal = %v, want 0.5", newTotal)
	}
	if count != 1 {
		t.Errorf("count = %v, want 1", count)
	}
	if txID == "" {
		t.Error("txID is empty")
	}
}

func TestCheckAndSpend_BudgetExceeded(t *testing.T) {
	store := newTestStore(t)

	// Spend 9.0 first
	ok, _, _, _, _, _ := store.CheckAndSpend("payer1", 9.0, 10.0, "/api/query")
	if !ok {
		t.Fatal("first spend should succeed")
	}

	// Try to spend 2.0 more (9.0 + 2.0 > 10.0)
	ok, reason, totalSpent, _, _, err := store.CheckAndSpend("payer1", 2.0, 10.0, "/api/query")
	if err != nil {
		t.Fatalf("CheckAndSpend() error: %v", err)
	}
	if ok {
		t.Error("should be budget exceeded")
	}
	if totalSpent != 9.0 {
		t.Errorf("totalSpent = %v, want 9.0", totalSpent)
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestCheckAndSpend_ExactLimit(t *testing.T) {
	store := newTestStore(t)

	ok, _, newTotal, _, _, _ := store.CheckAndSpend("payer1", 10.0, 10.0, "/api/query")
	if !ok {
		t.Error("spending exactly to limit should succeed")
	}
	if newTotal != 10.0 {
		t.Errorf("newTotal = %v, want 10.0", newTotal)
	}
}

func TestInsertAndGetAuditLogs(t *testing.T) {
	store := newTestStore(t)

	_ = store.InsertAuditLog("payment_success", 1.5, "success", "test", "payer1", "/api", "tx-1")
	_ = store.InsertAuditLog("budget_exceeded", 5.0, "rejected", "over limit", "payer1", "/api", "")
	_ = store.InsertAuditLog("approval_pending", 3.0, "pending", "high risk", "payer1", "/api", "tok-1")

	entries, _, err := store.GetAuditLogs(10, "", "")
	if err != nil {
		t.Fatalf("GetAuditLogs() error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	// Most recent first
	if entries[0].Action != "approval_pending" {
		t.Errorf("entries[0].Action = %q, want 'approval_pending'", entries[0].Action)
	}
}

func TestGetAuditLogs_ActionFilter(t *testing.T) {
	store := newTestStore(t)

	_ = store.InsertAuditLog("payment_success", 1.5, "success", "", "payer1", "/api", "tx-1")
	_ = store.InsertAuditLog("budget_exceeded", 5.0, "rejected", "", "payer1", "/api", "")
	_ = store.InsertAuditLog("payment_success", 2.0, "success", "", "payer1", "/api", "tx-2")

	entries, _, err := store.GetAuditLogs(10, "payment_success", "")
	if err != nil {
		t.Fatalf("GetAuditLogs() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
}

func TestGetAuditLogs_CursorPagination(t *testing.T) {
	store := newTestStore(t)

	// Insert 5 entries with slight time gaps
	for i := 0; i < 5; i++ {
		_ = store.InsertAuditLog("test", float64(i), "success", "", "payer1", "/api", "")
		time.Sleep(10 * time.Millisecond)
	}

	// Get first page of 2
	page1, cursor, err := store.GetAuditLogs(2, "", "")
	if err != nil {
		t.Fatalf("page1 error: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	if cursor == "" {
		t.Fatal("expected cursor for next page")
	}

	// Get second page
	page2, cursor2, err := store.GetAuditLogs(2, "", cursor)
	if err != nil {
		t.Fatalf("page2 error: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}

	// Get third page
	page3, cursor3, err := store.GetAuditLogs(2, "", cursor2)
	if err != nil {
		t.Fatalf("page3 error: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page3 len = %d, want 1", len(page3))
	}
	if cursor3 != "" {
		t.Error("expected no more cursor on last page")
	}
}

func TestCreateApprovalToken(t *testing.T) {
	store := newTestStore(t)

	token, err := store.CreateApprovalToken("payer1", 5.0, "/api/delete", "high_risk", `{"test":true}`)
	if err != nil {
		t.Fatalf("CreateApprovalToken() error: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("token length = %d, want 64", len(token))
	}

	status, payer, amount, resource, err := store.GetApprovalStatus(token)
	if err != nil {
		t.Fatalf("GetApprovalStatus() error: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want 'pending'", status)
	}
	if payer != "payer1" {
		t.Errorf("payer = %q, want 'payer1'", payer)
	}
	if amount != 5.0 {
		t.Errorf("amount = %v, want 5.0", amount)
	}
	if resource != "/api/delete" {
		t.Errorf("resource = %q, want '/api/delete'", resource)
	}
}

func TestGetApprovalStatus_NotFound(t *testing.T) {
	store := newTestStore(t)

	status, _, _, _, err := store.GetApprovalStatus("nonexistent")
	if err != nil {
		t.Fatalf("GetApprovalStatus() error: %v", err)
	}
	if status != "" {
		t.Errorf("status = %q, want empty for not found", status)
	}
}

func TestExpireOldTokens(t *testing.T) {
	store := newTestStore(t)

	// Create a token and manually expire it
	token, _ := store.CreateApprovalToken("payer1", 5.0, "/api", "test", "")

	// Set expires_at to the past
	_, _ = store.db.Exec(
		"UPDATE approval_token SET expires_at = ? WHERE token = ?",
		time.Now().Add(-1*time.Hour).Unix(), token,
	)

	count, err := store.ExpireOldTokens()
	if err != nil {
		t.Fatalf("ExpireOldTokens() error: %v", err)
	}
	if count != 1 {
		t.Errorf("expired count = %d, want 1", count)
	}

	status, _, _, _, _ := store.GetApprovalStatus(token)
	if status != "expired" {
		t.Errorf("status = %q, want 'expired'", status)
	}
}

func TestGetPendingApprovals(t *testing.T) {
	store := newTestStore(t)

	_, _ = store.CreateApprovalToken("payer1", 3.0, "/api/delete", "high_risk", "")
	_, _ = store.CreateApprovalToken("payer2", 5.0, "/api/send", "high_risk", "")

	approvals, err := store.GetPendingApprovals()
	if err != nil {
		t.Fatalf("GetPendingApprovals() error: %v", err)
	}
	if len(approvals) != 2 {
		t.Fatalf("len(approvals) = %d, want 2", len(approvals))
	}
}

func TestUpdateApprovalStatus(t *testing.T) {
	store := newTestStore(t)

	token, _ := store.CreateApprovalToken("payer1", 3.0, "/api", "test", "")

	err := store.UpdateApprovalStatus(token, "approved")
	if err != nil {
		t.Fatalf("UpdateApprovalStatus() error: %v", err)
	}

	status, _, _, _, _ := store.GetApprovalStatus(token)
	if status != "approved" {
		t.Errorf("status = %q, want 'approved'", status)
	}
}

package main

import (
	"testing"

	"go.uber.org/zap"
)

func TestIsHighRisk_AmountThreshold(t *testing.T) {
	if !IsHighRisk(3.0, "/api/query", 2.0, []string{"delete", "send"}) {
		t.Error("amount > threshold should be high risk")
	}
	if IsHighRisk(1.0, "/api/query", 2.0, []string{"delete", "send"}) {
		t.Error("amount <= threshold with safe resource should not be high risk")
	}
}

func TestIsHighRisk_ResourceKeywords(t *testing.T) {
	keywords := []string{"delete", "send"}
	if !IsHighRisk(0.5, "/api/delete/resource", 2.0, keywords) {
		t.Error("resource containing 'delete' should be high risk")
	}
	if !IsHighRisk(0.5, "/api/send-email", 2.0, keywords) {
		t.Error("resource containing 'send' should be high risk")
	}
	if IsHighRisk(0.5, "/api/query", 2.0, keywords) {
		t.Error("safe resource with low amount should not be high risk")
	}
}

func TestCreateApproval(t *testing.T) {
	store := newTestStore(t)
	cfg := &Config{BudgetLimit: 10.0}
	logger := zap.NewNop()
	token, err := CreateApproval(store, cfg, logger, "payer1", 5.0, "/api/delete")
	if err != nil {
		t.Fatalf("CreateApproval() error: %v", err)
	}
	if token == "" {
		t.Error("token is empty")
	}
	status, _, _, _, _ := store.GetApprovalStatus(token)
	if status != "pending" {
		t.Errorf("status = %q, want 'pending'", status)
	}
	logs, _, _ := store.GetAuditLogs(10, "", "")
	if len(logs) == 0 {
		t.Error("no audit log entries created")
	}
}

func TestResolveApproval_Reject(t *testing.T) {
	store := newTestStore(t)
	cfg := &Config{BudgetLimit: 10.0}
	logger := zap.NewNop()
	token, _ := CreateApproval(store, cfg, logger, "payer1", 5.0, "/api/delete")
	result, err := ResolveApproval(store, cfg, logger, token, false)
	if err != nil {
		t.Fatalf("ResolveApproval() error: %v", err)
	}
	if result.Status != "rejected" {
		t.Errorf("status = %q, want 'rejected'", result.Status)
	}
}

func TestResolveApproval_Approve(t *testing.T) {
	store := newTestStore(t)
	cfg := &Config{BudgetLimit: 10.0}
	logger := zap.NewNop()
	token, _ := CreateApproval(store, cfg, logger, "payer1", 3.0, "/api/delete")
	result, err := ResolveApproval(store, cfg, logger, token, true)
	if err != nil {
		t.Fatalf("ResolveApproval() error: %v", err)
	}
	if result.Status != "approved" {
		t.Errorf("status = %q, want 'approved'", result.Status)
	}
	if result.TransactionID == "" {
		t.Error("expected transaction_id after approved spend")
	}
}

func TestResolveApproval_BudgetExceededAfterApprove(t *testing.T) {
	store := newTestStore(t)
	cfg := &Config{BudgetLimit: 5.0}
	logger := zap.NewNop()
	ok, _, _, _, _, _ := store.CheckAndSpend("payer1", 4.0, 5.0, "/api/query")
	if !ok {
		t.Fatal("initial spend should succeed")
	}
	token, _ := CreateApproval(store, cfg, logger, "payer1", 3.0, "/api/delete")
	result, err := ResolveApproval(store, cfg, logger, token, true)
	if err != nil {
		t.Fatalf("ResolveApproval() error: %v", err)
	}
	if result.Status != "budget_exceeded" {
		t.Errorf("status = %q, want 'budget_exceeded'", result.Status)
	}
}

func TestResolveApproval_NotFound(t *testing.T) {
	store := newTestStore(t)
	cfg := &Config{BudgetLimit: 10.0}
	logger := zap.NewNop()
	result, err := ResolveApproval(store, cfg, logger, "nonexistent-token", true)
	if err != nil {
		t.Fatalf("ResolveApproval() error: %v", err)
	}
	if result.Status != "not_found" {
		t.Errorf("status = %q, want 'not_found'", result.Status)
	}
}

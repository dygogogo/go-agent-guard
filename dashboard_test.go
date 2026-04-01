package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func setupTestDashboard(t *testing.T) (BudgetStore, *Config, *zap.Logger) {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cfg := &Config{BudgetLimit: 10.0, HighRiskThreshold: 2.0, HighRiskResources: []string{"delete", "send"}}
	logger := zap.NewNop()
	return store, cfg, logger
}

func TestDashboard_MainPage(t *testing.T) {
	store, cfg, logger := setupTestDashboard(t)
	router := NewDashboardRouter(store, cfg, logger)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("dashboard status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "MCP Guard") || !strings.Contains(body, "Budget") || !strings.Contains(body, "Approvals") {
		t.Errorf("dashboard body missing expected elements")
	}
}

func TestDashboard_Approve(t *testing.T) {
	store, cfg, logger := setupTestDashboard(t)
	router := NewDashboardRouter(store, cfg, logger)
	// Create approval token
	token, err := CreateApproval(store, cfg, logger, "payer1", 5.0, "/api/delete")
	if err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	// Approve via dashboard
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/approve/"+token, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("approve status = %d, want 200", w.Code)
	}
}

func TestDashboard_Reject(t *testing.T) {
	store, cfg, logger := setupTestDashboard(t)
	router := NewDashboardRouter(store, cfg, logger)
	// Create approval token
	token, err := CreateApproval(store, cfg, logger, "payer1", 5.0, "/api/delete")
	if err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	// Reject via dashboard
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/reject/"+token, nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("reject status = %d, want 200", w.Code)
	}
}

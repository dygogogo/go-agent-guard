package main

import (
	"os"
	"testing"
)

func TestParseHighRiskResources(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"delete,send", []string{"delete", "send"}},
		{"  delete , send  ", []string{"delete", "send"}},
		{"DELETE,SEND", []string{"delete", "send"}},
		{"", []string{}},
		{",,,", []string{}},
		{"drop", []string{"drop"}},
	}

	for _, tt := range tests {
		got := parseHighRiskResources(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseHighRiskResources(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseHighRiskResources(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestResolvePayerID(t *testing.T) {
	// Save and restore
	orig := os.Getenv("PAYER_ID")
	defer os.Setenv("PAYER_ID", orig)

	os.Unsetenv("PAYER_ID")
	id := resolvePayerID()
	if id == "" {
		t.Error("resolvePayerID() returned empty string")
	}
	// Should be hostname since PAYER_ID is unset
	if id == "mcp-guard" {
		// hostname lookup might fail, fallback is ok
		t.Log("fell back to mcp-guard default")
	}

	os.Setenv("PAYER_ID", "my-agent")
	id = resolvePayerID()
	if id != "my-agent" {
		t.Errorf("resolvePayerID() = %q, want 'my-agent'", id)
	}
}

func TestGetEnv(t *testing.T) {
	os.Unsetenv("TEST_VAR_X")
	if v := getEnv("TEST_VAR_X", "default"); v != "default" {
		t.Errorf("getEnv unset = %q, want 'default'", v)
	}
	os.Setenv("TEST_VAR_X", "value")
	defer os.Unsetenv("TEST_VAR_X")
	if v := getEnv("TEST_VAR_X", "default"); v != "value" {
		t.Errorf("getEnv set = %q, want 'value'", v)
	}
}

func TestGetEnvAsFloat(t *testing.T) {
	os.Unsetenv("TEST_FLOAT_X")
	if v := getEnvAsFloat("TEST_FLOAT_X", 5.5); v != 5.5 {
		t.Errorf("getEnvAsFloat unset = %v, want 5.5", v)
	}
	os.Setenv("TEST_FLOAT_X", "10.5")
	defer os.Unsetenv("TEST_FLOAT_X")
	if v := getEnvAsFloat("TEST_FLOAT_X", 5.5); v != 10.5 {
		t.Errorf("getEnvAsFloat set = %v, want 10.5", v)
	}
	// Invalid float should return default
	os.Setenv("TEST_FLOAT_X", "not-a-number")
	if v := getEnvAsFloat("TEST_FLOAT_X", 5.5); v != 5.5 {
		t.Errorf("getEnvAsFloat invalid = %v, want 5.5", v)
	}
}

func TestGetEnvAsInt(t *testing.T) {
	os.Unsetenv("TEST_INT_X")
	if v := getEnvAsInt("TEST_INT_X", 42); v != 42 {
		t.Errorf("getEnvAsInt unset = %d, want 42", v)
	}
	os.Setenv("TEST_INT_X", "100")
	defer os.Unsetenv("TEST_INT_X")
	if v := getEnvAsInt("TEST_INT_X", 42); v != 100 {
		t.Errorf("getEnvAsInt set = %d, want 100", v)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Save and restore env
	envKeys := []string{
		"MCP_TRANSPORT", "BUDGET_LIMIT", "HIGH_RISK_THRESHOLD", "HIGH_RISK_RESOURCES",
		"DB_PATH", "DASHBOARD_PORT", "APPROVAL_BASE_URL", "LOG_LEVEL",
		"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "PAYER_ID",
	}
	for _, k := range envKeys {
		orig := os.Getenv(k)
		defer os.Setenv(k, orig)
		os.Unsetenv(k)
	}
	// Force stdio transport since stdin is not a tty in tests
	os.Setenv("MCP_TRANSPORT", "stdio")

	cfg := LoadConfig()
	if cfg.MCPTransport != "stdio" {
		t.Errorf("transport = %q, want stdio", cfg.MCPTransport)
	}
	if cfg.BudgetLimit != 10.0 {
		t.Errorf("budget_limit = %v, want 10.0", cfg.BudgetLimit)
	}
	if cfg.HighRiskThreshold != 2.0 {
		t.Errorf("high_risk_threshold = %v, want 2.0", cfg.HighRiskThreshold)
	}
	if cfg.DashboardPort != "8080" {
		t.Errorf("dashboard_port = %q, want 8080", cfg.DashboardPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("log_level = %q, want info", cfg.LogLevel)
	}
	if cfg.PayerID == "" {
		t.Error("payer_id should not be empty")
	}
}

func TestLoadConfig_FromEnv(t *testing.T) {
	// Move to clean dir to avoid .env file interference
	origDir, _ := os.Getwd()
	cleanDir := t.TempDir()
	if err := os.Chdir(cleanDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	envKeys := []string{
		"MCP_TRANSPORT", "BUDGET_LIMIT", "HIGH_RISK_THRESHOLD", "HIGH_RISK_RESOURCES",
		"DB_PATH", "DASHBOARD_PORT", "APPROVAL_BASE_URL", "LOG_LEVEL",
		"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "PAYER_ID",
	}
	for _, k := range envKeys {
		orig := os.Getenv(k)
		defer os.Setenv(k, orig)
		os.Unsetenv(k)
	}

	os.Setenv("MCP_TRANSPORT", "sse")
	os.Setenv("BUDGET_LIMIT", "50.0")
	os.Setenv("HIGH_RISK_THRESHOLD", "5.0")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("DASHBOARD_PORT", "9090")
	os.Setenv("PAYER_ID", "custom-payer")

	cfg := LoadConfig()
	if cfg.MCPTransport != "sse" {
		t.Errorf("transport = %q, want sse", cfg.MCPTransport)
	}
	if cfg.BudgetLimit != 50.0 {
		t.Errorf("budget = %v, want 50.0", cfg.BudgetLimit)
	}
	if cfg.HighRiskThreshold != 5.0 {
		t.Errorf("threshold = %v, want 5.0", cfg.HighRiskThreshold)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("db_path = %q, want /tmp/test.db", cfg.DBPath)
	}
	if cfg.DashboardPort != "9090" {
		t.Errorf("port = %q, want 9090", cfg.DashboardPort)
	}
	if cfg.PayerID != "custom-payer" {
		t.Errorf("payer = %q, want custom-payer", cfg.PayerID)
	}
}

func TestLoadEnvFile(t *testing.T) {
	// Create a temp .env file
	tmpDir := t.TempDir()
	envPath := tmpDir + "/.env"
	content := "TEST_ENV_VAR_1=hello\n# comment\nTEST_ENV_VAR_2=world\n\nTEST_ENV_VAR_3=123\n"
	if err := os.WriteFile(envPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Change to temp dir so loadEnvFile finds .env
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	os.Unsetenv("TEST_ENV_VAR_1")
	os.Unsetenv("TEST_ENV_VAR_2")
	os.Unsetenv("TEST_ENV_VAR_3")
	defer func() {
		os.Unsetenv("TEST_ENV_VAR_1")
		os.Unsetenv("TEST_ENV_VAR_2")
		os.Unsetenv("TEST_ENV_VAR_3")
	}()

	err := loadEnvFile()
	if err != nil {
		t.Fatalf("loadEnvFile error: %v", err)
	}
	if os.Getenv("TEST_ENV_VAR_1") != "hello" {
		t.Errorf("TEST_ENV_VAR_1 = %q, want hello", os.Getenv("TEST_ENV_VAR_1"))
	}
	if os.Getenv("TEST_ENV_VAR_2") != "world" {
		t.Errorf("TEST_ENV_VAR_2 = %q, want world", os.Getenv("TEST_ENV_VAR_2"))
	}
	if os.Getenv("TEST_ENV_VAR_3") != "123" {
		t.Errorf("TEST_ENV_VAR_3 = %q, want 123", os.Getenv("TEST_ENV_VAR_3"))
	}
}

func TestLoadEnvFile_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	err := loadEnvFile()
	if err != nil {
		t.Errorf("should not error when no .env file: %v", err)
	}
}

func TestAutoDetectTransport(t *testing.T) {
	// In test context, stdin is piped (not a tty), so should return "stdio"
	transport := autoDetectTransport()
	if transport != "stdio" {
		// This might be "http" if running in a terminal, so just log it
		t.Logf("autoDetectTransport = %q (depends on stdin)", transport)
	}
}

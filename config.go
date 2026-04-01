package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultBudgetLimit       = 10.0
	DefaultHighRiskThreshold = 2.0
	DefaultHighRiskResources = "delete,send"
	DefaultDBPath            = "./mcp-guard.db"
	DefaultLogLevel          = "info"
	DefaultDashboardPort     = "8080"
	DefaultApprovalBaseURL   = "http://localhost:8080"
)

// Config holds all configuration for mcp-guard.
type Config struct {
	// MCP transport: stdio | sse | http. Empty = auto-detect.
	MCPTransport string

	// Budget
	BudgetLimit       float64
	HighRiskThreshold float64
	HighRiskResources []string

	// Database
	DBPath string

	// Dashboard
	DashboardPort   string
	ApprovalBaseURL string

	// Logging
	LogLevel string

	// Telegram
	TelegramBotToken string
	TelegramChatID   string

	// Identity
	PayerID string
}

// LoadConfig reads configuration from environment variables.
// Falls back to .env file if present.
func LoadConfig() *Config {
	_ = loadEnvFile()

	transport := getEnv("MCP_TRANSPORT", "")
	if transport == "" {
		transport = autoDetectTransport()
	}

	return &Config{
		MCPTransport:      transport,
		BudgetLimit:       getEnvAsFloat("BUDGET_LIMIT", DefaultBudgetLimit),
		HighRiskThreshold: getEnvAsFloat("HIGH_RISK_THRESHOLD", DefaultHighRiskThreshold),
		HighRiskResources: parseHighRiskResources(getEnv("HIGH_RISK_RESOURCES", DefaultHighRiskResources)),
		DBPath:            getEnv("DB_PATH", DefaultDBPath),
		DashboardPort:     getEnv("DASHBOARD_PORT", DefaultDashboardPort),
		ApprovalBaseURL:   getEnv("APPROVAL_BASE_URL", DefaultApprovalBaseURL),
		LogLevel:          getEnv("LOG_LEVEL", DefaultLogLevel),
		TelegramBotToken:  getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:    getEnv("TELEGRAM_CHAT_ID", ""),
		PayerID:           resolvePayerID(),
	}
}

// autoDetectTransport returns "http" if stdin is a terminal, otherwise "stdio".
func autoDetectTransport() string {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "stdio"
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return "http"
	}
	return "stdio"
}

// resolvePayerID returns the payer identity following resolution order:
// 1. PAYER_ID env var
// 2. hostname
// 3. "mcp-guard" fallback
func resolvePayerID() string {
	if id := getEnv("PAYER_ID", ""); id != "" {
		return id
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	return "mcp-guard"
}

func parseHighRiskResources(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func loadEnvFile() error {
	if _, err := os.Stat(".env"); err != nil {
		return nil
	}
	data, err := os.ReadFile(".env")
	if err != nil {
		return fmt.Errorf("read .env: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsFloat(key string, defaultValue float64) float64 {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

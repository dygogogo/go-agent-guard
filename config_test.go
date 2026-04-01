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

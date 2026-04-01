package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ApprovalResult holds the outcome of resolving an approval request.
type ApprovalResult struct {
	Status        string  `json:"status"`
	Payer         string  `json:"payer"`
	Amount        float64 `json:"amount"`
	Resource      string  `json:"resource"`
	TotalSpent    float64 `json:"total_spent"`
	Remaining     float64 `json:"remaining"`
	TransactionID string  `json:"transaction_id,omitempty"`
	Message       string  `json:"message"`
}

// IsHighRisk checks if a spend request triggers the approval workflow.
func IsHighRisk(amount float64, resource string, threshold float64, keywords []string) bool {
	if amount > threshold {
		return true
	}
	lower := strings.ToLower(resource)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// CreateApproval creates a pending approval token and sends notifications.
func CreateApproval(store BudgetStore, cfg *Config, logger *zap.Logger, payer string, amount float64, resource string) (string, error) {
	token, err := store.CreateApprovalToken(payer, amount, resource, "high_risk",
		fmt.Sprintf(`{"payer":"%s","amount":%.2f,"resource":"%s"}`, payer, amount, resource))
	if err != nil {
		return "", fmt.Errorf("create approval token: %w", err)
	}

	if err := store.InsertAuditLog("approval_pending", amount, "pending",
		"high risk approval required", payer, resource, token); err != nil {
		logger.Error("audit log failed", zap.Error(err))
	}

	// Send Telegram notification (non-blocking, ignore errors)
	if err := SendTelegramNotification(cfg, logger, payer, amount, resource, token); err != nil {
		logger.Warn("telegram notification failed", zap.Error(err))
	}

	return token, nil
}

// ResolveApproval handles approve/reject of a pending token, optionally executing the spend.
func ResolveApproval(store BudgetStore, cfg *Config, logger *zap.Logger, token string, approved bool) (*ApprovalResult, error) {
	status, payer, amount, resource, err := store.GetApprovalStatus(token)
	if err != nil {
		return nil, fmt.Errorf("get approval status: %w", err)
	}
	if status == "" {
		return &ApprovalResult{Status: "not_found", Message: "token not found"}, nil
	}
	if status != "pending" {
		return &ApprovalResult{Status: status, Message: fmt.Sprintf("token already %s", status)}, nil
	}

	if !approved {
		if err := store.UpdateApprovalStatus(token, "rejected"); err != nil {
			return nil, fmt.Errorf("update approval status: %w", err)
		}
		_ = store.InsertAuditLog("approval_rejected", amount, "rejected",
			"approval rejected", payer, resource, token)
		logger.Info("approval rejected",
			zap.String("payer", payer), zap.Float64("amount", amount), zap.String("token", token))

		return &ApprovalResult{
			Status:   "rejected",
			Payer:    payer,
			Amount:   amount,
			Resource: resource,
			Message:  "approval rejected",
		}, nil
	}

	// Approve: execute the spend
	if err := store.UpdateApprovalStatus(token, "approved"); err != nil {
		return nil, fmt.Errorf("update approval status: %w", err)
	}

	ok, reason, newTotal, _, txID, spendErr := store.CheckAndSpend(payer, amount, cfg.BudgetLimit, resource)
	if spendErr != nil {
		_ = store.UpdateApprovalStatus(token, "error")
		return nil, fmt.Errorf("spend after approval: %w", spendErr)
	}

	if !ok {
		_ = store.UpdateApprovalStatus(token, "budget_exceeded")
		_ = store.InsertAuditLog("approval_budget_exceeded", amount, "rejected",
			reason, payer, resource, token)
		return &ApprovalResult{
			Status:   "budget_exceeded",
			Payer:    payer,
			Amount:   amount,
			Resource: resource,
			Message:  reason,
		}, nil
	}

	_ = store.InsertAuditLog("approval_payment_success", amount, "success",
		"approved payment executed", payer, resource, txID)
	logger.Info("approval payment success",
		zap.String("payer", payer), zap.Float64("amount", amount), zap.String("txID", txID))

	return &ApprovalResult{
		Status:        "approved",
		Payer:         payer,
		Amount:        amount,
		Resource:      resource,
		TotalSpent:    newTotal,
		Remaining:     cfg.BudgetLimit - newTotal,
		TransactionID: txID,
		Message:       "approved and payment executed",
	}, nil
}

// SendTelegramNotification sends an approval request via Telegram.
func SendTelegramNotification(cfg *Config, logger *zap.Logger, payer string, amount float64, resource string, token string) error {
	if cfg.TelegramBotToken == "" || cfg.TelegramChatID == "" {
		return nil
	}

	approveURL := fmt.Sprintf("%s/approve/%s", cfg.ApprovalBaseURL, token)
	rejectURL := fmt.Sprintf("%s/reject/%s", cfg.ApprovalBaseURL, token)

	message := fmt.Sprintf(
		"*High Risk Approval Request*\n\n"+
			"*Payer:* `%s`\n"+
			"*Amount:* %.2f USDC\n"+
			"*Resource:* `%s`\n\n"+
			"[Approve](%s)  |  [Reject](%s)",
		payer, amount, resource, approveURL, rejectURL,
	)

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramBotToken)
	reqBody := map[string]interface{}{
		"chat_id":    cfg.TelegramChatID,
		"text":       message,
		"parse_mode": "Markdown",
	}

	reqBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(reqBytes)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("telegram send failed",
			zap.Int("status", resp.StatusCode), zap.String("response", string(body)))
		return fmt.Errorf("telegram API error: %d", resp.StatusCode)
	}

	logger.Info("telegram notification sent",
		zap.String("payer", payer), zap.Float64("amount", amount))
	return nil
}

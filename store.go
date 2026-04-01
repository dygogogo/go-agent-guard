package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// BudgetStore abstracts all database operations.
type BudgetStore interface {
	Close() error
	GetDailySpent() (spent float64, count int, err error)
	CheckAndSpend(payer string, amount float64, budgetLimit float64, resource string) (ok bool, reason string, newTotal float64, count int, txID string, err error)
	InsertAuditLog(action string, amount float64, status string, details string, payer string, resource string, transactionID string) error
	GetAuditLogs(limit int, actionFilter string, cursor string) (entries []AuditLogEntry, nextCursor string, err error)
	CreateApprovalToken(payer string, amount float64, resource string, action string, requestData string) (token string, err error)
	GetApprovalStatus(token string) (status string, payer string, amount float64, resource string, err error)
	UpdateApprovalStatus(token string, status string) error
	ExpireOldTokens() (count int, err error)
	GetPendingApprovals() ([]PendingApproval, error)
}

// AuditLogEntry represents a single audit log row.
type AuditLogEntry struct {
	ID            int     `json:"id"`
	Timestamp     string  `json:"timestamp"`
	Action        string  `json:"action"`
	Amount        float64 `json:"amount"`
	Status        string  `json:"status"`
	Details       string  `json:"details"`
	Payer         string  `json:"payer"`
	Resource      string  `json:"resource"`
	TransactionID string  `json:"transaction_id"`
	CreatedAt     int64   `json:"created_at"`
}

// PendingApproval represents a pending approval token.
type PendingApproval struct {
	Token     string  `json:"token"`
	Payer     string  `json:"payer"`
	Amount    float64 `json:"amount"`
	Resource  string  `json:"resource"`
	Action    string  `json:"action"`
	CreatedAt string  `json:"created_at"`
}

// SQLiteStore implements BudgetStore using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens a SQLite database and initializes the schema.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	dsn := dbPath
	if dbPath != ":memory:" {
		dsn = dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS daily_budget (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL UNIQUE,
			total_spent REAL NOT NULL DEFAULT 0,
			request_count INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS spending_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			payer TEXT NOT NULL,
			amount REAL NOT NULL,
			resource TEXT,
			transaction_id TEXT,
			success INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			action TEXT NOT NULL,
			amount REAL,
			status TEXT NOT NULL,
			details TEXT,
			payer TEXT,
			resource TEXT,
			transaction_id TEXT,
			created_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS approval_token (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT NOT NULL UNIQUE,
			payer TEXT NOT NULL,
			amount REAL NOT NULL,
			resource TEXT NOT NULL,
			action TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			request_data TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_daily_budget_date ON daily_budget(date)`,
		`CREATE INDEX IF NOT EXISTS idx_spending_log_date ON spending_log(date)`,
		`CREATE INDEX IF NOT EXISTS idx_spending_log_payer ON spending_log(payer)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_timestamp ON audit_log(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_action ON audit_log(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_token_token ON approval_token(token)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_token_status ON approval_token(status)`,
	}
	for _, s := range schemas {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) GetDailySpent() (float64, int, error) {
	today := time.Now().Format("2006-01-02")
	var totalSpent float64
	var count int
	err := s.db.QueryRow(
		"SELECT total_spent, request_count FROM daily_budget WHERE date = ?", today,
	).Scan(&totalSpent, &count)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return totalSpent, count, nil
}

func (s *SQLiteStore) CheckAndSpend(payer string, amount float64, budgetLimit float64, resource string) (bool, string, float64, int, string, error) {
	today := time.Now().Format("2006-01-02")
	now := time.Now().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return false, "database error", 0, 0, "", err
	}
	defer tx.Rollback()

	var totalSpent float64
	var requestCount int
	err = tx.QueryRow(
		"SELECT total_spent, request_count FROM daily_budget WHERE date = ?", today,
	).Scan(&totalSpent, &requestCount)
	if err == sql.ErrNoRows {
		totalSpent = 0
		requestCount = 0
		_, err = tx.Exec(
			"INSERT INTO daily_budget (date, total_spent, request_count, created_at, updated_at) VALUES (?, 0, 0, ?, ?)",
			today, now, now,
		)
		if err != nil {
			return false, "database error", 0, 0, "", err
		}
	} else if err != nil {
		return false, "database error", 0, 0, "", err
	}

	newTotal := totalSpent + amount
	if newTotal > budgetLimit {
		reason := fmt.Sprintf("%.2f + %.2f > %.2f USDC", totalSpent, amount, budgetLimit)
		return false, reason, totalSpent, requestCount, "", nil
	}

	_, err = tx.Exec(
		"UPDATE daily_budget SET total_spent = ?, request_count = request_count + 1, updated_at = ? WHERE date = ?",
		newTotal, now, today,
	)
	if err != nil {
		return false, "database error", 0, 0, "", err
	}

	txID := fmt.Sprintf("tx-%d-%d", now, time.Now().Nanosecond())
	_, err = tx.Exec(
		"INSERT INTO spending_log (date, payer, amount, resource, transaction_id, success, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)",
		today, payer, amount, resource, txID, now,
	)
	if err != nil {
		return false, "database error", 0, 0, "", err
	}

	if err := tx.Commit(); err != nil {
		return false, "database error", 0, 0, "", err
	}

	return true, "", newTotal, requestCount + 1, txID, nil
}

func (s *SQLiteStore) InsertAuditLog(action string, amount float64, status string, details string, payer string, resource string, transactionID string) error {
	now := time.Now()
	_, err := s.db.Exec(
		"INSERT INTO audit_log (timestamp, action, amount, status, details, payer, resource, transaction_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		now.Format(time.RFC3339), action, amount, status, details, payer, resource, transactionID, now.Unix(),
	)
	return err
}

func (s *SQLiteStore) GetAuditLogs(limit int, actionFilter string, cursor string) ([]AuditLogEntry, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var args []interface{}
	var conditions []string
	var joinAnd string

	if actionFilter != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, actionFilter)
	}

	var cursorTs int64
	var cursorID int
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(cursor)
		if err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				fmt.Sscanf(parts[0], "%d", &cursorTs)
				fmt.Sscanf(parts[1], "%d", &cursorID)
				conditions = append(conditions, "(created_at < ? OR (created_at = ? AND id < ?))")
				args = append(args, cursorTs, cursorTs, cursorID)
			}
		}
	}

	query := "SELECT id, timestamp, action, amount, status, details, payer, resource, transaction_id, created_at FROM audit_log"
	if len(conditions) > 0 {
		joinAnd = " WHERE " + strings.Join(conditions, " AND ")
	}
	query += joinAnd + " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var entries []AuditLogEntry
	for rows.Next() {
		var e AuditLogEntry
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Action, &e.Amount, &e.Status, &e.Details, &e.Payer, &e.Resource, &e.TransactionID, &e.CreatedAt); err != nil {
			return nil, "", err
		}
		entries = append(entries, e)
	}

	var nextCursor string
	if len(entries) > limit {
		last := entries[limit-1]
		nextCursor = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", last.CreatedAt, last.ID)))
		entries = entries[:limit]
	}

	return entries, nextCursor, nil
}

func (s *SQLiteStore) CreateApprovalToken(payer string, amount float64, resource string, action string, requestData string) (string, error) {
	token := generateToken()
	now := time.Now()
	expiresAt := now.Add(30 * time.Minute).Unix()

	_, err := s.db.Exec(
		"INSERT INTO approval_token (token, payer, amount, resource, action, status, created_at, expires_at, request_data) VALUES (?, ?, ?, ?, ?, 'pending', ?, ?, ?)",
		token, payer, amount, resource, action, now.Unix(), expiresAt, requestData,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *SQLiteStore) GetApprovalStatus(token string) (string, string, float64, string, error) {
	var payer, resource, status string
	var amount float64
	var expiresAt int64

	err := s.db.QueryRow(
		"SELECT payer, amount, resource, status, expires_at FROM approval_token WHERE token = ?", token,
	).Scan(&payer, &amount, &resource, &status, &expiresAt)
	if err == sql.ErrNoRows {
		return "", "", 0, "", nil
	}
	if err != nil {
		return "", "", 0, "", err
	}

	// Auto-expire if past TTL
	if status == "pending" && time.Now().Unix() > expiresAt {
		_ = s.UpdateApprovalStatus(token, "expired")
		return "expired", payer, amount, resource, nil
	}

	return status, payer, amount, resource, nil
}

func (s *SQLiteStore) UpdateApprovalStatus(token string, status string) error {
	_, err := s.db.Exec("UPDATE approval_token SET status = ? WHERE token = ?", status, token)
	return err
}

func (s *SQLiteStore) ExpireOldTokens() (int, error) {
	now := time.Now().Unix()
	result, err := s.db.Exec(
		"UPDATE approval_token SET status = 'expired' WHERE status = 'pending' AND expires_at < ?", now,
	)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) GetPendingApprovals() ([]PendingApproval, error) {
	// Lazy cleanup first
	_, _ = s.ExpireOldTokens()

	rows, err := s.db.Query(`
		SELECT token, payer, amount, resource, action, datetime(created_at, 'unixepoch') as created_at
		FROM approval_token
		WHERE status = 'pending'
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []PendingApproval
	for rows.Next() {
		var a PendingApproval
		if err := rows.Scan(&a.Token, &a.Payer, &a.Amount, &a.Resource, &a.Action, &a.CreatedAt); err != nil {
			return nil, err
		}
		approvals = append(approvals, a)
	}
	return approvals, nil
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback should never happen with crypto/rand
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

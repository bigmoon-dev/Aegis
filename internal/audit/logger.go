package audit

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/bigmoon-dev/agent-harness/internal/model"
	_ "github.com/mattn/go-sqlite3"
)

// Logger writes audit entries and rate limit records to SQLite.
type Logger struct {
	db     *sql.DB
	mu     sync.RWMutex // RWMutex: RLock for reads, Lock for writes
	stopCh chan struct{}
}

// NewLogger opens (or creates) the SQLite database at dbPath.
func NewLogger(dbPath string) (*Logger, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}
	if _, err := db.Exec(createTableSQL); err != nil {
		return nil, fmt.Errorf("create audit tables: %w", err)
	}
	if _, err := db.Exec(createRateLimitTableSQL); err != nil {
		return nil, fmt.Errorf("create rate limit tables: %w", err)
	}
	return &Logger{db: db, stopCh: make(chan struct{})}, nil
}

// Log writes an audit entry.
func (l *Logger) Log(e *model.AuditEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.db.Exec(insertSQL,
		e.RequestID, e.AgentID, e.BackendID, e.ToolName, e.Arguments,
		e.ACLResult, e.RateLimitResult, e.ApprovalResult,
		e.QueuePosition, e.QueueWaitMs,
		e.ExecStatus, e.ExecDurationMs, e.ErrorMessage,
	)
	if err != nil {
		log.Printf("[audit] write error: %v", err)
	}
}

// RecordCall records a tool call for rate limiting.
func (l *Logger) RecordCall(agentID, toolName string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.db.Exec(insertRateLimitSQL, agentID, toolName)
	if err != nil {
		log.Printf("[audit] record rate limit error: %v", err)
	}
}

// CountCalls returns the number of calls by an agent for a tool since the given time.
func (l *Logger) CountCalls(agentID, toolName string, since time.Time) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var count int
	err := l.db.QueryRow(countRateLimitSQL, agentID, toolName, since.UTC().Format("2006-01-02 15:04:05")).Scan(&count)
	if err != nil {
		log.Printf("[audit] count rate limit error: %v", err)
		return 0
	}
	return count
}

// CountCallsGlobal returns the number of calls for a tool across all agents since the given time.
func (l *Logger) CountCallsGlobal(toolName string, since time.Time) int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var count int
	err := l.db.QueryRow(countRateLimitGlobalSQL, toolName, since.UTC().Format("2006-01-02 15:04:05")).Scan(&count)
	if err != nil {
		log.Printf("[audit] count global rate limit error: %v", err)
		return 0
	}
	return count
}

// AuditRow represents a row from the audit log for API queries.
type AuditRow struct {
	RequestID       string `json:"request_id"`
	AgentID         string `json:"agent_id"`
	BackendID       string `json:"backend_id"`
	ToolName        string `json:"tool_name"`
	Arguments       string `json:"arguments"`
	ACLResult       string `json:"acl_result"`
	RateLimitResult string `json:"rate_limit_result"`
	ApprovalResult  string `json:"approval_result"`
	QueuePosition   int    `json:"queue_position"`
	QueueWaitMs     int64  `json:"queue_wait_ms"`
	ExecStatus      string `json:"exec_status"`
	ExecDurationMs  int64  `json:"exec_duration_ms"`
	ErrorMessage    string `json:"error_message"`
	CreatedAt       string `json:"created_at"`
}

// Query returns recent audit log rows.
func (l *Logger) Query(limit, offset int) ([]AuditRow, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	rows, err := l.db.Query(querySQL, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]AuditRow, 0)
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(
			&r.RequestID, &r.AgentID, &r.BackendID, &r.ToolName, &r.Arguments,
			&r.ACLResult, &r.RateLimitResult, &r.ApprovalResult,
			&r.QueuePosition, &r.QueueWaitMs,
			&r.ExecStatus, &r.ExecDurationMs, &r.ErrorMessage, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// Purge deletes audit entries older than the given number of days.
func (l *Logger) Purge(days int) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	res, err := l.db.Exec(purgeSQL, fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, err
	}
	// Also purge old rate limit entries (same retention as audit log)
	if _, err := l.db.Exec(purgeRateLimitSQL, fmt.Sprintf("-%d days", days)); err != nil {
		log.Printf("[audit] purge rate limit entries error: %v", err)
	}
	return res.RowsAffected()
}

// StartPurgeLoop starts a background goroutine that purges old entries daily.
func (l *Logger) StartPurgeLoop(retentionDays int) {
	go func() {
		// Run once at startup
		if n, err := l.Purge(retentionDays); err != nil {
			log.Printf("[audit] startup purge error: %v", err)
		} else if n > 0 {
			log.Printf("[audit] startup purge: removed %d old entries", n)
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n, err := l.Purge(retentionDays); err != nil {
					log.Printf("[audit] purge error: %v", err)
				} else if n > 0 {
					log.Printf("[audit] purge: removed %d old entries", n)
				}
			case <-l.stopCh:
				return
			}
		}
	}()
}

// Close stops the purge loop and closes the database.
func (l *Logger) Close() error {
	close(l.stopCh)
	return l.db.Close()
}

package audit

const createTableSQL = `
CREATE TABLE IF NOT EXISTS audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    request_id      TEXT UNIQUE,
    agent_id        TEXT,
    backend_id      TEXT,
    tool_name       TEXT,
    arguments       TEXT,
    acl_result      TEXT,
    rate_limit_result TEXT,
    approval_result TEXT,
    queue_position  INTEGER,
    queue_wait_ms   INTEGER,
    exec_status     TEXT,
    exec_duration_ms INTEGER,
    error_message   TEXT,
    created_at      DATETIME DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit_log(agent_id);
CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit_log(tool_name);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at);
`

const insertSQL = `
INSERT INTO audit_log (
    request_id, agent_id, backend_id, tool_name, arguments,
    acl_result, rate_limit_result, approval_result,
    queue_position, queue_wait_ms,
    exec_status, exec_duration_ms, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

const purgeSQL = `DELETE FROM audit_log WHERE created_at < datetime('now', ?)`

const querySQL = `
SELECT request_id, agent_id, backend_id, tool_name, arguments,
       acl_result, rate_limit_result, approval_result,
       queue_position, queue_wait_ms,
       exec_status, exec_duration_ms, error_message, created_at
FROM audit_log ORDER BY id DESC LIMIT ? OFFSET ?
`

// Rate limiter persistence
const createRateLimitTableSQL = `
CREATE TABLE IF NOT EXISTS rate_limit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id   TEXT NOT NULL,
    tool_name  TEXT NOT NULL,
    called_at  DATETIME DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_rl_agent_tool ON rate_limit_log(agent_id, tool_name);
CREATE INDEX IF NOT EXISTS idx_rl_called ON rate_limit_log(called_at);
`

const insertRateLimitSQL = `INSERT INTO rate_limit_log (agent_id, tool_name) VALUES (?, ?)`

const countRateLimitSQL = `
SELECT COUNT(*) FROM rate_limit_log
WHERE agent_id = ? AND tool_name = ? AND called_at >= ?
`

const countRateLimitGlobalSQL = `
SELECT COUNT(*) FROM rate_limit_log
WHERE tool_name = ? AND called_at >= ?
`

const purgeRateLimitSQL = `DELETE FROM rate_limit_log WHERE called_at < datetime('now', ?)`

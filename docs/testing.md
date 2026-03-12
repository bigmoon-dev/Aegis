# Aegis MCP — Testing

## Overview

| Metric | Value |
|--------|-------|
| Framework | Go standard `testing` package |
| Test files | 14 (`_test.go`) + 1 test helper |
| Test cases | 155 |
| Coverage | **89.3%** (statement-level) |
| Race Detector | All passing |
| CI | `.github/workflows/ci.yml` (push/PR trigger) |

---

## Running Tests

```bash
# Run all tests
CGO_ENABLED=1 go test ./... -count=1

# With race detector
CGO_ENABLED=1 go test -race ./... -count=1

# With coverage
CGO_ENABLED=1 go test -cover ./internal/...

# Verbose output for a specific package
CGO_ENABLED=1 go test -v ./internal/pipeline/ -count=1

# Single test
CGO_ENABLED=1 go test -v -run TestRateLimiter_DBError_FailClosed ./internal/pipeline/

# Generate coverage report
CGO_ENABLED=1 go test -coverprofile=cover.out ./internal/...
go tool cover -func=cover.out        # text report
go tool cover -html=cover.out        # HTML report
```

**Note**: `CGO_ENABLED=1` is required (SQLite depends on cgo).

---

## Test Architecture

### Dependency Isolation

| Dependency | Isolation Method |
|------------|-----------------|
| SQLite | `t.TempDir()` + temp DB file, auto-cleaned after test |
| Config | `config.NewManagerFromConfig(cfg)` builds from struct, no YAML files |
| Backend MCP Server | `ForwardFunc` function signature injection (`echoForward`, `slowForward`) |
| HTTP Backend | `httptest.Server` simulates MCP backends with real HTTP |
| Webhooks | `httptest.Server` simulates Feishu/Generic webhook endpoints |
| HTTP Handler | `httptest.NewRequest` + `httptest.NewRecorder` white-box testing |
| Time | Real time with ms-level delays, avoids fake clock complexity |

### File Structure

```
internal/
├── api/
│   └── router_test.go            # 15 tests — REST API endpoints
├── approval/
│   ├── store_test.go             # 12 tests — approval store + HMAC tokens
│   ├── callback_test.go          #  9 tests — approval callback handler
│   └── notifier_test.go          # 13 tests — Feishu/Generic/Multi notifiers
├── audit/
│   └── logger_test.go            #  9 tests — audit log CRUD + rate limit counting
├── config/
│   ├── testing.go                # NewManagerFromConfig() test helper
│   └── config_test.go            # 15 tests — config validation + Manager load/reload
├── model/
│   └── model_test.go             #  6 tests — JSON-RPC types
├── pipeline/
│   ├── acl_test.go               #  6 tests — access control
│   ├── approval_test.go          #  7 tests — approval gate
│   ├── ratelimit_test.go         # 11 tests — rate limiting
│   ├── queue_test.go             #  9 tests — FIFO queue
│   └── pipeline_test.go          #  6 tests — end-to-end pipeline
├── proxy/
│   ├── handler_test.go           # 18 tests — HTTP handler + routing
│   ├── forwarder_test.go         #  9 tests — backend forwarding
│   ├── toolslist_test.go         # 12 tests — tool list enhancement
│   └── session_test.go           #  2 tests — session management
```

---

## Coverage Details

### By Package

| Package | Coverage | Tests | Notes |
|---------|----------|-------|-------|
| `internal/model` | **100.0%** | 6 | Full coverage |
| `internal/config` | **98.7%** | 15 | Near-complete |
| `internal/api` | **95.3%** | 15 | All REST endpoints |
| `internal/approval` | **94.7%** | 34 | Store/Callback/Notifier |
| `internal/proxy` | **88.5%** | 41 | Handler/Forwarder/ToolsList |
| `internal/pipeline` | **88.0%** | 39 | ACL/RateLimiter/Approval/Queue |
| `internal/audit` | **68.6%** | 9 | Core CRUD covered |

### Security-Critical Path Coverage

| Path | Coverage | Notes |
|------|----------|-------|
| HMAC token validation | **100%** | Constant-time comparison via `hmac.Equal()` |
| ACL access control | **100%** | All deny paths produce correct error codes |
| Rate limiting (fail-closed) | **96.8%** | DB failure → deny (not allow) |
| Approval callback auth | **100%** | Invalid/missing token → 403 |
| SQL injection prevention | **Safe** | All queries use parameterized `?` placeholders |

### Not Covered

| Function | Reason |
|----------|--------|
| `audit.StartPurgeLoop` | Background goroutine + ticker loop, requires time mock |
| `pipeline.worker` (partial) | Shutdown signal timing branches |
| `proxy.handlePassthrough` error path | Requires precise backend error simulation |

---

## Test Conventions

1. **File naming**: `{source}_test.go` in the same package (white-box testing, access to unexported members)
2. **Helpers**: Marked with `t.Helper()`, use `t.TempDir()` and `t.Cleanup()` for automatic cleanup
3. **Config building**: Use `config.NewManagerFromConfig()` instead of writing temp YAML files
4. **Forward mocking**: Via `ForwardFunc` function signature injection, no interface mocks needed
5. **HTTP mocking**: `httptest.Server` for backends and webhooks, `httptest.NewRecorder` for handlers
6. **Concurrency safety**: Mock types use `sync.Mutex` to protect shared state (e.g., `mockNotifier`)
7. **Timeouts**: Queue delays set to ms-level (1-20ms) in tests to avoid slow test suites
8. **Concurrent tests**: Use `sync.WaitGroup` + buffered channels to collect errors
9. **Synchronization**: Polling loops with deadlines instead of `time.Sleep` for robustness
10. **Race Detector**: CI runs with `-race` by default, all tests must pass

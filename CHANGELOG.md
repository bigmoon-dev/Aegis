# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [v0.1.8] - 2026-03-14

### Added
- `aegis setup` interactive configuration wizard
  - Auto-discovers tools from MCP backend via protocol handshake
  - Smart defaults based on tool name patterns (read-only, search, write, dangerous)
  - Agent adapter injection for OpenClaw (mcporter.json) and Claude Code (mcp_servers.json)
  - Conflict detection when agent config already has an entry for the same backend
  - Automatic `.bak` backup before modifying agent config files
  - Global rate limits auto-calculated at 2× agent limits for multi-agent headroom
- 27 new tests for setup package (integration, edge cases, unit)

### Changed
- New dependencies: `charmbracelet/huh` v0.5.3 (interactive CLI forms), `golang.org/x/text` (display name casing)

### Fixed
- `resolveQueueDelays` maxDelay calculation bug
- Replaced deprecated `strings.Title` with `cases.Title` from `golang.org/x/text`

## [v0.1.7] - 2026-03-12

### Fixed
- Apply `gofmt -s` formatting across 6 files (Go Report Card: 0 issues)

## [v0.1.6] - 2026-03-12

### Added
- Management API authentication via `server.api_token` with constant-time Bearer token comparison
- 157 unit tests across all packages (89.3% coverage)
- CI coverage reporting with GitHub Step Summary
- README badges (CI, coverage, Go Report Card, license)
- `docs/testing.md` — testing architecture and conventions
- CHANGELOG.md

### Fixed
- Queue race condition in FIFO queue shutdown
- Rate limiter now fail-closed (denies on DB error instead of allowing)
- Test reliability: replaced `time.Sleep` with polling loops

## [v0.1.5] - 2026-03-12

### Added
- Interactive demo: `aegis demo` launches mock MCP server + proxy with pre-configured policy
- Demo covers ACL filtering, rate limiting, human approval, queue bypass, and audit logging
- Works with both `./aegis demo` and `npx aegis-mcp-proxy demo`

### Fixed
- Explicit `case 0` in notifier switch to avoid fallthrough

## [v0.1.4] - 2026-03-12

### Added
- Generic webhook notifier for approval notifications (Slack, Discord, custom systems)
- Feishu and generic webhook can be configured simultaneously

### Changed
- Upgraded GitHub Actions to Node 24 versions

## [v0.1.3] - 2026-03-11

### Changed
- Renamed npm package to `aegis-mcp-proxy` (npm similarity protection blocked `aegis-mcp`)

## [v0.1.2] - 2026-03-11

### Added
- Multi-channel distribution: npm (`aegis-mcp-proxy`), Docker (`ghcr.io/bigmoon-dev/aegis`), `go install`

## [v0.1.1] - 2026-03-11

### Added
- GitHub Actions release workflow for cross-platform binaries (Linux/macOS/Windows, amd64/arm64)

## [v0.1.0] - 2026-03-11

### Added
- Initial release as Aegis MCP
- MCP protocol-level transparent proxy between AI agents and tool servers
- Access Control (ACL) — per-agent, per-backend, per-tool allow/deny rules
- Two-level rate limiting — per-agent sliding window + global cross-agent limits
- Human approval workflows via Feishu/Lark webhooks with HMAC-signed callbacks
- FIFO execution queue with configurable random delays per backend
- Audit logging to SQLite with auto-purge and configurable retention
- Tool description enhancement with constraint annotations
- Hot reload via `POST /api/v1/config/reload`
- Policy configuration guide (English + Chinese)
- Bilingual documentation (README.md + README_CN.md)

[Unreleased]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.8...HEAD
[v0.1.8]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.7...v0.1.8
[v0.1.7]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.6...v0.1.7
[v0.1.6]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.5...v0.1.6
[v0.1.5]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.4...v0.1.5
[v0.1.4]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.3...v0.1.4
[v0.1.3]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.2...v0.1.3
[v0.1.2]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.1...v0.1.2
[v0.1.1]: https://github.com/bigmoon-dev/Aegis/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/bigmoon-dev/Aegis/releases/tag/v0.1.0

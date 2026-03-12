# Contributing to Aegis MCP

Thank you for your interest in contributing!

## Development Setup

```bash
# Requirements: Go 1.22+, GCC (for CGO/SQLite)
git clone https://github.com/bigmoon-dev/Aegis.git
cd Aegis
make build
```

## Running Tests

```bash
# All tests (requires CGO)
CGO_ENABLED=1 go test ./... -count=1

# With race detector (recommended)
CGO_ENABLED=1 go test -race ./... -count=1

# With coverage
CGO_ENABLED=1 go test -cover ./internal/...
```

Tests use temporary SQLite databases and in-memory configs — no external dependencies required.

## Code Style

- Run `gofmt -s` before committing
- Run `go vet ./...` to check for issues
- Add godoc comments to all exported types and functions
- Follow existing patterns in the codebase

## Submitting Changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Make your changes with tests
4. Ensure all tests pass: `CGO_ENABLED=1 go test -race ./... -count=1`
5. Submit a pull request

## Reporting Issues

Please open an issue on [GitHub Issues](https://github.com/bigmoon-dev/Aegis/issues) with:

- A clear description of the problem
- Steps to reproduce
- Expected vs actual behavior
- Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the [AGPL-3.0](LICENSE) license.

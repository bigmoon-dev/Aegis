BINARY := agent-harness
CMD := ./cmd/harness/

.PHONY: build run clean test lint cross-rpi

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY) config/harness.example.yaml

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f $(BINARY)

# Cross-compile for Raspberry Pi (ARM64)
# Requires cross-compiler: sudo apt install gcc-aarch64-linux-gnu
cross-rpi:
	CGO_ENABLED=1 CC=aarch64-linux-gnu-gcc GOOS=linux GOARCH=arm64 go build -o $(BINARY) $(CMD)
	@echo "Built $(BINARY) for linux/arm64"

# Initialize config from example
init-config:
	@if [ ! -f config/harness.yaml ]; then \
		cp config/harness.example.yaml config/harness.yaml; \
		echo "Created config/harness.yaml from example"; \
	else \
		echo "config/harness.yaml already exists"; \
	fi

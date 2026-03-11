BINARY := aegis
CMD := ./cmd/aegis/

.PHONY: build run clean test lint cross-rpi init-config

build:
	go build -trimpath -ldflags "-s -w" -o $(BINARY) $(CMD)

run: build
	./$(BINARY) config/aegis.example.yaml

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
	@if [ ! -f config/aegis.yaml ]; then \
		cp config/aegis.example.yaml config/aegis.yaml; \
		echo "Created config/aegis.yaml from example"; \
	else \
		echo "config/aegis.yaml already exists"; \
	fi

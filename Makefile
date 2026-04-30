BINARY    := monitoring24
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build run run-auth dev cross-linux cross-linux-arm64 cross-windows test vet clean install-service

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/server

run: build
	./$(BINARY)

run-auth: build
	./$(BINARY) --basic-auth admin:changeme

dev:
	@AIR_BIN="$$(command -v air || true)"; \
	if [ -z "$$AIR_BIN" ] && [ -x "$$(go env GOPATH)/bin/air" ]; then \
		AIR_BIN="$$(go env GOPATH)/bin/air"; \
	fi; \
	if [ -z "$$AIR_BIN" ]; then \
		echo "Installing air (live reload watcher)..."; \
		go install github.com/air-verse/air@latest; \
		AIR_BIN="$$(go env GOPATH)/bin/air"; \
	fi; \
	"$$AIR_BIN" -c .air.toml

cross-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) \
		-o $(BINARY)-linux-amd64 ./cmd/server
	@echo "Built: $(BINARY)-linux-amd64"

cross-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) \
		-o $(BINARY)-linux-arm64 ./cmd/server
	@echo "Built: $(BINARY)-linux-arm64"

cross-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) \
		-o $(BINARY)-windows-amd64.exe ./cmd/server
	@echo "Built: $(BINARY)-windows-amd64.exe"

cross-all: cross-linux cross-linux-arm64 cross-windows

test:
	go test ./... -race -count=1

vet:
	go vet ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux-* $(BINARY)-windows-*

# Run on the Linux deployment target after cross-compiling
install-service: cross-linux
	install -m 755 $(BINARY)-linux-amd64 /usr/local/bin/$(BINARY)
	install -m 644 monitoring24.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable $(BINARY)
	systemctl start $(BINARY)
	@echo "Service installed and started"

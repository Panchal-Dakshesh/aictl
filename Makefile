VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test install fixtures dist

build:
	go build -ldflags "$(LDFLAGS)" -o aictl ./cmd/aictl

test:
	go vet ./...
	go test ./...

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/aictl

# Regenerate testdata/home (made-up sessions for every tool).
fixtures:
	python3 scripts/mkfixtures.py

dist:
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/aictl-darwin-arm64 ./cmd/aictl
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/aictl-darwin-amd64 ./cmd/aictl
	GOOS=linux  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/aictl-linux-amd64 ./cmd/aictl
	GOOS=linux  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/aictl-linux-arm64 ./cmd/aictl

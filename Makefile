VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -ldflags "-X github.com/hdck007/yeet/internal/cli.Version=$(VERSION)"
BINARY = yeet

.PHONY: build install test clean

build:
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BINARY) ./cmd/yeet/

install:
	CGO_ENABLED=1 go install $(LDFLAGS) ./cmd/yeet/

test:
	go test ./...

clean:
	rm -f $(BINARY)
	go clean

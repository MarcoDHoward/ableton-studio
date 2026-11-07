.PHONY: build test lint install clean

BIN_EXT := $(if $(filter windows,$(shell go env GOOS)),.exe,)

build:
	go build -trimpath -o bin/ableton-pp-cli$(BIN_EXT) ./cmd/ableton-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/ableton-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -trimpath -o bin/ableton-pp-mcp$(BIN_EXT) ./cmd/ableton-pp-mcp

install-mcp:
	go install ./cmd/ableton-pp-mcp

build-all: build build-mcp

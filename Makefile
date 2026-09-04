# quietdm — daemon and Neovim frontend.

GO      ?= go
NVIM    ?= nvim
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test test-go test-lua fmt vet clean run-mock

all: build

build: ## Build the daemon into ./quietdmd
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o quietdmd ./cmd/quietdmd

test: test-go test-lua

test-go:
	$(GO) test -race ./...

test-lua:
	$(NVIM) --headless -u tests/minimal_init.lua -c "luafile tests/run.lua"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

clean:
	rm -f quietdmd

# Start the daemon with the demo script, on a socket under /tmp.
run-mock: build
	./quietdmd -config examples/mock.toml -socket /tmp/quietdm-demo.sock -v

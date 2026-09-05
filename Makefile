# quietdm — daemon and Neovim frontend.

GO      ?= go
NVIM    ?= nvim
# mautrix-go links against libolm unless this tag is set; goolm is the pure-Go
# implementation, which keeps the daemon a single static binary with no cgo.
TAGS    ?= goolm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all build test test-go test-lua test-e2e fmt vet clean run-mock

all: build

build: ## Build the daemon into ./quietdmd
	$(GO) build -tags $(TAGS) -ldflags "-X main.version=$(VERSION)" -o quietdmd ./cmd/quietdmd

test: test-go test-lua test-e2e

test-go:
	$(GO) test -tags $(TAGS) -race ./...

test-lua:
	$(NVIM) --headless -u tests/minimal_init.lua -c "luafile tests/run.lua"

test-e2e: build ## Run the daemon and the frontend against each other
	NVIM=$(NVIM) ./scripts/e2e.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet -tags $(TAGS) ./...

clean:
	rm -f quietdmd

# Start the daemon with the demo script, on the default socket path so the
# frontend needs no configuration at all.
run-mock: build
	./quietdmd -config examples/mock.toml -v

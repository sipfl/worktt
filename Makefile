BINARY  := worktt
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install run fmt vet check clean

build: ## Compile the binary into ./$(BINARY)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install: ## Install into $GOPATH/bin (~/go/bin)
	go install -ldflags "$(LDFLAGS)" .

run: build ## Build and run (pass args via ARGS="-date 2026-06-15")
	./$(BINARY) $(ARGS)

fmt: ## Format sources
	gofmt -w .

vet: ## Static analysis
	go vet ./...

check: fmt vet build ## Format, vet and build

clean: ## Remove the built binary
	rm -f $(BINARY)

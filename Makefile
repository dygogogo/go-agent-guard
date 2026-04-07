.PHONY: build run test clean vet cover docker

BINARY   := agent-guard-mcp
GO       := go
GOFLAGS  := -trimpath -ldflags="-s -w"

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) .

run: build
	./$(BINARY)

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

cover:
	$(GO) test -race -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

clean:
	rm -f $(BINARY) coverage.out

docker:
	docker build -t agent-guard-mcp .

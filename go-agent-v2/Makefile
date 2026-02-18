.PHONY: build run test vet clean protocol-sync-check

build:
	go build ./...

run:
	go run ./cmd/server/main.go

mcp:
	go run ./cmd/mcp-server/main.go

test:
	go test ./... -race -count=1

protocol-sync-check:
	go test ./internal/codex -run TestProtocolMethodCoverage_FromCodexRs -count=1
	go test ./internal/apiserver -run TestEventMethodMap_TargetMethodsKnownByProtocol -count=1

vet:
	go vet ./...

clean:
	rm -rf bin/

fmt:
	goimports -w .

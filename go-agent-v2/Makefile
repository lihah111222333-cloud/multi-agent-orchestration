.PHONY: build run test vet clean

build:
	go build ./...

run:
	go run ./cmd/server/main.go

mcp:
	go run ./cmd/mcp-server/main.go

test:
	go test ./... -race -count=1

vet:
	go vet ./...

clean:
	rm -rf bin/

fmt:
	goimports -w .

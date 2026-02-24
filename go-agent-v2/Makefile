.PHONY: build run test vet clean protocol-sync-check ui-cover-build ui-cover-run ui-cover-report app-cover-build app-cover-run app-cover-report

MIN_MACOS_VERSION ?= 11.0

ifeq ($(shell uname -s),Darwin)
export CGO_CFLAGS := $(strip $(CGO_CFLAGS) -mmacosx-version-min=$(MIN_MACOS_VERSION))
export CGO_CXXFLAGS := $(strip $(CGO_CXXFLAGS) -mmacosx-version-min=$(MIN_MACOS_VERSION))
export CGO_LDFLAGS := $(strip $(CGO_LDFLAGS) -mmacosx-version-min=$(MIN_MACOS_VERSION))
endif

build:
	go build ./...

run:
	go run ./cmd/server/main.go

mcp:
	go run ./cmd/mcp-server/main.go

test:
	go test ./... -race -count=1

test-e2e:
	go test -tags=e2e ./cmd/rpc-test/ -v -timeout 120s -count=1

protocol-sync-check:
	go test ./internal/codex -run TestProtocolMethodCoverage_FromCodexRs -count=1
	go test ./internal/apiserver -run TestEventMethodMap_TargetMethodsKnownByProtocol -count=1

vet:
	go vet ./...

clean:
	rm -rf bin/

fmt:
	goimports -w .

ui-cover-build:
	./scripts/ui-coverage.sh build

ui-cover-run:
	./scripts/ui-coverage.sh run --debug

ui-cover-report:
	./scripts/ui-coverage.sh report

app-cover-build:
	TARGET=app-server ./scripts/ui-coverage.sh build

app-cover-run:
	TARGET=app-server ./scripts/ui-coverage.sh run --listen ws://127.0.0.1:4500

app-cover-report:
	TARGET=app-server ./scripts/ui-coverage.sh report

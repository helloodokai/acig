.PHONY: build test lint clean release-snapshot schema

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o dist/acig ./cmd/acig

test:
	go test ./...

lint:
	golangci-lint run

vet:
	go vet ./...

clean:
	rm -rf dist/

release-snapshot:
	goreleaser release --snapshot --clean

schema:
	@mkdir -p schema
	@go run ./cmd/acig schema > schema/verdict.schema.json
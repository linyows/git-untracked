TEST ?= ./...

default: build

build:
	env CGO_ENABLED=0 go build -ldflags="-s -w" -o git-untracked ./cmd/git-untracked

test:
	go test -race $(TEST) $(TESTARGS) -coverprofile=coverage.out -covermode=atomic

lint:
	golangci-lint run ./...

xbuild:
	goreleaser release --snapshot --clean

.PHONY: default build test lint xbuild

BIN      := sapper
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X github.com/sapper-unbuilt/sapper/internal/cli.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test check cross clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/sapper

test:
	go test -race ./...

check:
	gofmt -l . | (! grep .)
	go vet ./...
	staticcheck ./...
	go test -race ./...

cross:
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		out=dist/$(BIN)-$$os-$$arch$$ext; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/sapper; \
		echo "$$out"; \
	done

clean:
	rm -rf bin dist

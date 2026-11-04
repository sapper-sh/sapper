BIN      := sapper
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -buildid= -X github.com/sapper-unbuilt/sapper/internal/cli.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: build test check cross clean repro fips

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

# repro builds linux/amd64 twice in separate output dirs and fails unless the
# binaries are byte-identical.
repro:
	@set -e; rm -rf dist/repro; mkdir -p dist/repro/a dist/repro/b; \
	for d in a b; do CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/repro/$$d/$(BIN) ./cmd/sapper; done; \
	sha256sum dist/repro/a/$(BIN) dist/repro/b/$(BIN); cmp dist/repro/a/$(BIN) dist/repro/b/$(BIN) && echo reproducible

# fips links the frozen Go Cryptographic Module v1.0.0 (CMVP #5247). See docs/FIPS.md.
fips:
	GOFIPS140=v1.0.0 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BIN)-fips ./cmd/sapper

clean:
	rm -rf bin dist

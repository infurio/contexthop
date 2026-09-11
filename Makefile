TOOLCHAIN ?= go$(shell cat .go-version)
GOCACHE ?= $(CURDIR)/.gocache

.PHONY: build test vet check clean check-format check-zsh race ci

build:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go build -trimpath -buildvcs=false -o bin/chop ./cmd/contexthop

test:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go test ./...

vet:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go vet ./...

check: test vet build

clean:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go clean -cache

check-format:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

check-zsh:
	@test "$$(uname -s)" = Darwin || { echo "CI requires macOS"; exit 1; }
	@command -v zsh >/dev/null || { echo "CI requires zsh; shell tests must not skip"; exit 1; }
	@zsh --version

race:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go test -race ./...

ci: check-zsh check-format check race

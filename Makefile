TOOLCHAIN ?= go$(shell cat .go-version)
GOCACHE ?= $(CURDIR)/.gocache

.PHONY: build test vet check clean check-format check-zsh release

build:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go build -trimpath -buildvcs=false -o bin/chop ./cmd/contexthop

test:
	@work=$$(mktemp -d); trap 'rm -rf "$$work"' EXIT; \
	mkdir -p "$$work/home" "$$work/bin"; \
	for provider in gcloud kubectl docker chop; do \
	  printf '#!/bin/sh\nexit 1\n' > "$$work/bin/$$provider"; chmod +x "$$work/bin/$$provider"; \
	done; \
	env -i HOME="$$work/home" PATH="$$work/bin:$(PATH)" \
	  GOPATH="$$(go env GOPATH)" GOMODCACHE="$$(go env GOMODCACHE)" \
	  GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go test -race ./...

vet:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go vet ./...

check: check-zsh check-format test vet
	python3 -m unittest discover -s scripts/tests -p 'test_*.py'

clean:
	GOTOOLCHAIN=$(TOOLCHAIN) GOCACHE=$(GOCACHE) go clean -cache

check-format:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

check-zsh:
	@test "$$(uname -s)" = Darwin || { echo "Tests require macOS"; exit 1; }
	@command -v zsh >/dev/null || { echo "Tests require zsh; shell tests must not skip"; exit 1; }
	@zsh --version

release:
	@test -n "$(VERSION)" || { echo "Usage: make release VERSION=vX.Y.Z"; exit 1; }
	./scripts/release-local "$(VERSION)"

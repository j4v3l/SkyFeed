GO ?= go

.PHONY: build check fmt fmt-check govulncheck licenses race release-check staticcheck test vet

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/skyfeed ./cmd/skyfeed
	$(GO) build -trimpath -o bin/skyfeed-agent ./cmd/skyfeed-agent

fmt:
	gofmt -w $$(find cmd internal test -name '*.go' -type f)

fmt-check:
	@test -z "$$(gofmt -l .)"

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) tool staticcheck ./...

govulncheck:
	$(GO) tool govulncheck ./...

licenses:
	$(GO) tool go-licenses check ./cmd/skyfeed
	$(GO) tool go-licenses report ./cmd/skyfeed
	$(GO) tool go-licenses check ./cmd/skyfeed-agent
	$(GO) tool go-licenses report ./cmd/skyfeed-agent

release-check:
	@sh scripts/release-version.sh >/dev/null

check: fmt-check release-check test race vet staticcheck govulncheck licenses

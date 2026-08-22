GO ?= go

.PHONY: build check fmt fmt-check govulncheck licenses race staticcheck test vet

build:
	mkdir -p bin
	$(GO) build -trimpath -o bin/skyfeed ./cmd/skyfeed

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

check: fmt-check test race vet staticcheck govulncheck licenses

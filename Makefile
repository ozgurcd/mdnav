SHELL := /bin/sh
export CGO_ENABLED := 0

.PHONY: help build test fmt-check release-notes-check release-check verify install-tools

RELEASE_NOTES_OUTPUT ?= /dev/null
GORELEASER_VERSION := v2.17.0

help:
	@printf '%s\n' 'Targets:' \
		'  build               build ./cmd/mdnav' \
		'  test                run unit tests' \
		'  fmt-check           fail on unformatted Go files' \
		'  release-notes-check run the release extractor for the source version' \
		'  release-check       check configuration with the workflow-pinned GoReleaser via go run' \
		'  verify              run the complete local verification gate' \
		'  install-tools       install pinned verification tools'

build:
	go build ./...

test:
	go test ./... -count=1 -timeout=120s

fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }

release-notes-check:
	go run ./cmd/release-notes RELEASE_NOTES.md > "$(RELEASE_NOTES_OUTPUT)"

release-check:
	go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) check

# Each step checks something no other step does: vet and test compile every
# package, so there is no separate build; release notes are checked by the
# release workflow, which extracts them.
verify: fmt-check
	go test ./... -count=1 -timeout=120s
	go vet ./...
	STATICCHECK_CACHE=$(CURDIR)/.cache/staticcheck staticcheck ./...
	govulncheck ./...
	go mod tidy -diff

install-tools:
	@staticcheck --version | grep -F "staticcheck 2026.2.1 (0.8.1)"
	go install golang.org/x/vuln/cmd/govulncheck@v1.3.0

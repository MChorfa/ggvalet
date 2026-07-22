BINARY := ggvalet
PKG := github.com/MChorfa/ggvalet
DIST := dist
# Version comes from the git tag (GA = a real semver tag, e.g. v1.0.0).
# The fallback marks an untagged dev build, never a release.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build install test cover cover-p19 vwp fmt vet clean tidy cross checksums sbom release release-dry goreleaser-check site attest-sign

all: tidy fmt vet build

build:
	go build $(LDFLAGS) -o $(BINARY) .

install:
	go install $(LDFLAGS) .

test:
	go test ./...

# Full suite + coverage profile + 80% gate (mirrors the CI `test` job).
cover:
	go test ./... -coverprofile=cover.out -covermode=atomic
	go tool cover -func=cover.out | tee coverage.txt
	@total=$$(awk '/^total:/ {gsub("%","",$$3); print $$3}' coverage.txt); \
	echo "total coverage: $$total% (gate: 80%)"; \
	awk -v t="$$total" 'BEGIN { exit !(t+0 >= 80.0) }' \
		|| { echo "FAIL: coverage $$total% < 80%"; exit 1; }

# P19 trust/reconciliation slice. This does not replace the repository-wide
# `cover` gate; it proves the newly introduced critical path independently.
cover-p19:
	go test ./internal/state ./internal/observed ./internal/plan ./internal/reconcile \
		-coverprofile=p19-cover.out -covermode=atomic
	go tool cover -func=p19-cover.out | tee p19-coverage.txt
	@total=$$(awk '/^total:/ {gsub("%","",$$3); print $$3}' p19-coverage.txt); \
	echo "P19 critical coverage: $$total% (gate: 80%)"; \
	awk -v t="$$total" 'BEGIN { exit !(t+0 >= 80.0) }' \
		|| { echo "FAIL: P19 coverage $$total% < 80%"; exit 1; }

# CKODEX VWP §26 governance gate (mirrors the CI `vwp` job).
vwp:
	bash scripts/vwp-lint.sh

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BINARY) glv gitlabvalet
	rm -rf dist/ site/ public/
	rm -f sbom.cdx.json
	rm -f cover.out coverage.txt p19-cover.out p19-coverage.txt
	rm -f *.test *.out cmd.out

# Cross-compile for common platforms
cross:
	mkdir -p $(DIST)
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-arm64 .
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-darwin-amd64 .
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-linux-arm64 .
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST)/$(BINARY)-windows-amd64.exe .
	@echo "Cross-compiled binaries in $(DIST)/"

# SHA-256 checksums for every release binary (portable: sha256sum or shasum).
checksums:
	cd $(DIST) && { command -v sha256sum >/dev/null 2>&1 && sha256sum $(BINARY)-* || shasum -a 256 $(BINARY)-*; } > SHA256SUMS
	@echo "Wrote $(DIST)/SHA256SUMS"

# CycloneDX SBOM of the build (Syft). Supply-chain evidence per CLAUDE.md §15.
sbom:
	syft dir:. -o cyclonedx-json=$(DIST)/sbom.cdx.json
	@echo "Wrote $(DIST)/sbom.cdx.json"

# Local release bundle (UNSIGNED). The CI `release` job cosign-signs SHA256SUMS;
# this target proves the artifact assembly works without the OIDC signing step.
release: clean cross checksums sbom
	@echo "── release bundle ($(VERSION)) ──"
	@ls -1 $(DIST)
	@echo "Sign in CI: cosign sign-blob (keyless) → SHA256SUMS.sig + .pem"

# Cosign-sign the VWP attestation. Keyless in CI (SIGSTORE_ID_TOKEN present);
# locally pass a key, e.g. `cosign sign-blob --key cosign.key ...`.
attest-sign:
	cosign sign-blob --yes docs/VWP-ATTESTATION.md \
		--output-signature docs/VWP-ATTESTATION.md.sig \
		--output-certificate docs/VWP-ATTESTATION.md.pem
	@echo "Signed docs/VWP-ATTESTATION.md → .sig + .pem"

# Validate GoReleaser configuration. The GitLab variant requires a git repo
# with a remote; it is validated by the release pipeline.
goreleaser-check:
	goreleaser check --config .goreleaser.yml

# Dry-run GoReleaser release (snapshot, no publish, no sign) for local testing.
release-dry:
	goreleaser release --clean --snapshot --skip=sign --config .goreleaser.yml

# Build the static documentation site for local preview.
site:
	bash scripts/build-site.sh --output site

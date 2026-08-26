GO ?= go
GOFMT ?= gofmt
GOVULNCHECK_VERSION ?= v1.7.0
ACTIONLINT_VERSION ?= v1.7.12
FUZZ_PARALLELISM ?= 4
RELEASE_TOOLS_DIR ?= $(CURDIR)/.cache/release-tools
GORELEASER ?= $(RELEASE_TOOLS_DIR)/goreleaser
SYFT ?= $(RELEASE_TOOLS_DIR)/syft

.PHONY: verify verify-module verify-format verify-vet verify-test verify-fuzz verify-vulnerability verify-workflows verify-distribution verify-docs verify-shell benchmark canary ai-canary agentic-canary e2e release-tools release-ensure-tools release-snapshot release-reproducible release-tag release-tag-reproducible verify-release container-snapshot homebrew-formula verify-go-install

verify:
	$(MAKE) verify-module
	$(MAKE) verify-format
	$(MAKE) verify-vet
	$(MAKE) verify-test
	$(MAKE) verify-fuzz
	$(MAKE) verify-vulnerability
	$(MAKE) verify-workflows
	$(MAKE) verify-distribution
	$(MAKE) verify-docs
	$(MAKE) benchmark
	$(MAKE) verify-shell

verify-module:
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum

verify-format:
	@command -v "$(GOFMT)" >/dev/null 2>&1 || { \
		echo "gofmt executable not found: $(GOFMT)" >&2; \
		exit 1; \
	}
	@unformatted="$$($(GOFMT) -l .)" || exit $$?; \
	test -z "$$unformatted" || { \
		echo "Go files require formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	}

verify-vet:
	$(GO) vet ./...

verify-test:
	$(GO) test -race ./...

verify-fuzz:
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzParseConfigDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzParseChangeSetDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./internal/snapshot -run=^$$ -fuzz=FuzzParseSnapshotDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./pkg/promql -run=^$$ -fuzz=FuzzAnalyze -fuzztime=5s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./pkg/traceql -run=^$$ -fuzz=FuzzAnalyzeDoesNotPanic -fuzztime=5s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/weaver -run=^$$ -fuzz=FuzzParseDiffDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/weaver -run=^$$ -fuzz=FuzzParseMappingDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/persesusage -run=^$$ -fuzz=FuzzDecodeMetricsDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/runtimequeries -run=^$$ -fuzz=FuzzDecodePrometheusQueryLog -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/runtimequeries -run=^$$ -fuzz=FuzzDecodeTMRQueryHistory -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/keda -run=^$$ -fuzz=FuzzParseDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/argorollouts -run=^$$ -fuzz=FuzzParseDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/hpa -run=^$$ -fuzz=FuzzParseMappingDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/hpa -run=^$$ -fuzz=FuzzParseManifestDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/cloudformation -run=^$$ -fuzz=FuzzParseTemplateDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./adapters/cloudformation -run=^$$ -fuzz=FuzzParseManifestDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./internal/explanation -run=^$$ -fuzz=FuzzDecodeResponseDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)
	$(GO) test ./internal/remediation -run=^$$ -fuzz=FuzzDecodeResponseDoesNotPanic -fuzztime=3s -parallel=$(FUZZ_PARALLELISM)

verify-vulnerability:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

verify-workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -color
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -color release-fixtures/external-consumer-repository/.github/workflows/consumer.yml
	./scripts/check-workflow-policy.sh

verify-distribution:
	./scripts/check-release-coordinates.sh
	GO="$(GO)" ./scripts/verify-consumer-fixtures.sh
	GO="$(GO)" ./scripts/run-product-canaries.sh

verify-docs:
	GO="$(GO)" ./scripts/verify-docs.sh

benchmark:
	GO="$(GO)" ./benchmarks/scripts/run.sh

canary:
	GO="$(GO)" ./scripts/run-product-canaries.sh
	$(MAKE) ai-canary

ai-canary:
	GO="$(GO)" ./scripts/run-ai-canaries.sh

verify-shell:
	bash -n action/run-action.sh action/build-action.sh action/resolve-action-paths.sh benchmarks/scripts/run.sh e2e/scripts/*.sh experiments/agentic/testdata/run-canary.sh scripts/build-container-snapshot.sh scripts/check-release-coordinates.sh scripts/check-workflow-policy.sh scripts/generate-homebrew-formula.sh scripts/install-release-tools.sh scripts/build-release.sh scripts/run-ai-canaries.sh scripts/run-product-canaries.sh scripts/verify-consumer-fixtures.sh scripts/verify-container.sh scripts/verify-docs.sh scripts/verify-go-install.sh scripts/verify-release.sh scripts/verify-reproducible-release.sh scripts/validate-release-tag.sh

e2e:
	./e2e/scripts/run-control-plane-e2e.sh
	./e2e/scripts/run-e2e.sh
	./e2e/scripts/run-tempo-e2e.sh
	$(MAKE) agentic-canary

agentic-canary:
	GO_COMMAND="$(GO)" ./experiments/agentic/testdata/run-canary.sh

release-tools:
	./scripts/install-release-tools.sh "$(RELEASE_TOOLS_DIR)"

release-ensure-tools:
	@if ! command -v "$(GORELEASER)" >/dev/null 2>&1 || ! command -v "$(SYFT)" >/dev/null 2>&1; then \
		$(MAKE) release-tools; \
	fi

release-snapshot: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/build-release.sh snapshot

release-reproducible: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/verify-reproducible-release.sh snapshot

# This target never publishes. It requires the exact annotated candidate tag;
# only the gated tag workflow is allowed to create the GitHub prerelease.
release-tag: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/build-release.sh tag

release-tag-reproducible: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/verify-reproducible-release.sh tag

verify-release:
	GO="$(GO)" ./scripts/verify-release.sh

container-snapshot:
	GO="$(GO)" ./scripts/build-container-snapshot.sh

homebrew-formula:
	./scripts/generate-homebrew-formula.sh

verify-go-install:
	@test -n "$(REF)" || { echo "usage: make verify-go-install REF=<git-ref> [VERSION=<expected-version>]" >&2; exit 2; }
	GO="$(GO)" ./scripts/verify-go-install.sh "$(REF)" "$(VERSION)"

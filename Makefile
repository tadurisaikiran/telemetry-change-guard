GO ?= go
GOFMT ?= $(dir $(GO))gofmt
GOVULNCHECK_VERSION ?= v1.7.0
ACTIONLINT_VERSION ?= v1.7.12
RELEASE_TOOLS_DIR ?= $(CURDIR)/.cache/release-tools
GORELEASER ?= $(RELEASE_TOOLS_DIR)/goreleaser
SYFT ?= $(RELEASE_TOOLS_DIR)/syft

.PHONY: verify verify-module verify-format verify-vet verify-test verify-fuzz verify-vulnerability verify-workflows verify-shell e2e release-tools release-ensure-tools release-snapshot release-tag verify-release

verify:
	$(MAKE) verify-module
	$(MAKE) verify-format
	$(MAKE) verify-vet
	$(MAKE) verify-test
	$(MAKE) verify-fuzz
	$(MAKE) verify-vulnerability
	$(MAKE) verify-workflows
	$(MAKE) verify-shell

verify-module:
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum

verify-format:
	@test -z "$$($(GOFMT) -l .)" || { \
		echo "Go files require formatting:"; \
		$(GOFMT) -l .; \
		exit 1; \
	}

verify-vet:
	$(GO) vet ./...

verify-test:
	$(GO) test -race ./...

verify-fuzz:
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzParseConfigDoesNotPanic -fuzztime=3s
	$(GO) test ./internal/config -run=^$$ -fuzz=FuzzParseChangeSetDoesNotPanic -fuzztime=3s
	$(GO) test ./internal/snapshot -run=^$$ -fuzz=FuzzParseSnapshotDoesNotPanic -fuzztime=3s
	$(GO) test ./pkg/promql -run=^$$ -fuzz=FuzzAnalyze -fuzztime=5s
	$(GO) test ./pkg/traceql -run=^$$ -fuzz=FuzzAnalyzeDoesNotPanic -fuzztime=5s
	$(GO) test ./adapters/weaver -run=^$$ -fuzz=FuzzParseDiffDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/weaver -run=^$$ -fuzz=FuzzParseMappingDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/persesusage -run=^$$ -fuzz=FuzzDecodeMetricsDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/runtimequeries -run=^$$ -fuzz=FuzzDecodePrometheusQueryLog -fuzztime=3s
	$(GO) test ./adapters/runtimequeries -run=^$$ -fuzz=FuzzDecodeTMRQueryHistory -fuzztime=3s
	$(GO) test ./adapters/keda -run=^$$ -fuzz=FuzzParseDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/argorollouts -run=^$$ -fuzz=FuzzParseDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/hpa -run=^$$ -fuzz=FuzzParseMappingDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/hpa -run=^$$ -fuzz=FuzzParseManifestDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/cloudformation -run=^$$ -fuzz=FuzzParseTemplateDoesNotPanic -fuzztime=3s
	$(GO) test ./adapters/cloudformation -run=^$$ -fuzz=FuzzParseManifestDoesNotPanic -fuzztime=3s
	$(GO) test ./internal/explanation -run=^$$ -fuzz=FuzzDecodeResponseDoesNotPanic -fuzztime=3s
	$(GO) test ./internal/remediation -run=^$$ -fuzz=FuzzDecodeResponseDoesNotPanic -fuzztime=3s

verify-vulnerability:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

verify-workflows:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION) -color
	./scripts/check-workflow-policy.sh

verify-shell:
	bash -n action/run-action.sh action/build-action.sh action/resolve-action-paths.sh scripts/check-workflow-policy.sh scripts/install-release-tools.sh scripts/build-release.sh scripts/verify-release.sh scripts/validate-release-tag.sh

e2e:
	./e2e/scripts/run-control-plane-e2e.sh
	./e2e/scripts/run-e2e.sh
	./e2e/scripts/run-tempo-e2e.sh

release-tools:
	./scripts/install-release-tools.sh "$(RELEASE_TOOLS_DIR)"

release-ensure-tools:
	@if ! command -v "$(GORELEASER)" >/dev/null 2>&1 || ! command -v "$(SYFT)" >/dev/null 2>&1; then \
		$(MAKE) release-tools; \
	fi

release-snapshot: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/build-release.sh snapshot

# This target never publishes. It requires the exact annotated candidate tag;
# only the gated tag workflow is allowed to create the GitHub prerelease.
release-tag: release-ensure-tools
	GO="$(GO)" GORELEASER="$(GORELEASER)" SYFT="$(SYFT)" ./scripts/build-release.sh tag

verify-release:
	GO="$(GO)" ./scripts/verify-release.sh

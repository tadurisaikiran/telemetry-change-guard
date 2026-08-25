GO ?= go
GOFMT ?= $(dir $(GO))gofmt
GOVULNCHECK_VERSION ?= v1.7.0

.PHONY: verify verify-module verify-format verify-vet verify-test verify-fuzz verify-vulnerability verify-shell e2e

verify:
	$(MAKE) verify-module
	$(MAKE) verify-format
	$(MAKE) verify-vet
	$(MAKE) verify-test
	$(MAKE) verify-fuzz
	$(MAKE) verify-vulnerability
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

verify-shell:
	bash -n action/run-action.sh

e2e:
	./e2e/scripts/run-control-plane-e2e.sh
	./e2e/scripts/run-e2e.sh
	./e2e/scripts/run-tempo-e2e.sh

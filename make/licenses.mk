.PHONY: licenses licenses-check

# Third-party license policy. This allowlist is the single source of
# truth; a dependency under any other license (including an unknown one)
# fails licenses-check and needs a human decision, never a silent
# exception.
LICENSES_ALLOWED := Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC
LICENSES_REPORT := THIRD_PARTY_LICENSES.txt
LICENSES_TEMPLATE := make/third_party_licenses.tpl

# Pinned in tools/go.mod. GOOS/GOARCH are fixed so the report does not
# depend on the machine generating it (build-tag-selected dependencies
# differ per platform); linux/amd64 matches the production image.
# Test-only imports are excluded: they are not distributed. Our own
# module is ignored (its license is LICENSE), but its dependencies are
# still scanned. klog's per-file assembly warnings are silenced; errors
# still reach stderr.
GO_LICENSES = GOOS=linux GOARCH=amd64 $(GO_TOOL) go-licenses
# Global flags; go-licenses only accepts them after the subcommand.
GO_LICENSES_FLAGS = --logtostderr=false --stderrthreshold=ERROR --log_file=/dev/null \
	--ignore "$$(go list -m)"

# licenses regenerates THIRD_PARTY_LICENSES.txt (committed; distributed
# in the production image). Written through a temp file so a failed run
# never leaves a truncated report behind.
licenses:
	@set -e; tmp="$$(mktemp)"; trap 'rm -f "$$tmp"' EXIT; \
	$(GO_LICENSES) report ./... $(GO_LICENSES_FLAGS) --template $(LICENSES_TEMPLATE) > "$$tmp"; \
	cat "$$tmp" > $(LICENSES_REPORT); \
	echo "wrote $(LICENSES_REPORT)"

# licenses-check fails if any dependency's license is outside
# LICENSES_ALLOWED or cannot be identified, or if the committed report
# is stale.
licenses-check:
	$(GO_LICENSES) check ./... $(GO_LICENSES_FLAGS) --allowed_licenses=$(LICENSES_ALLOWED)
	@set -e; tmp="$$(mktemp)"; trap 'rm -f "$$tmp"' EXIT; \
	$(GO_LICENSES) report ./... $(GO_LICENSES_FLAGS) --template $(LICENSES_TEMPLATE) > "$$tmp"; \
	diff -u $(LICENSES_REPORT) "$$tmp" > /dev/null || \
		{ echo "$(LICENSES_REPORT) is stale: run 'make licenses' and commit the result" >&2; exit 1; }

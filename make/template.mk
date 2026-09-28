.PHONY: template-check

# template-check smoke-tests scripts/init-template.sh on a temporary copy
# of the repository. Template-only: init-template.sh removes this file.
template-check:
	./scripts/template-check.sh

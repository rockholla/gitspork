# A useful test while developing locally
.PHONY: dev-test-integrate
dev-test-integrate:
	@if [ ! -d /tmp/gitspork-downstream ]; then \
		mkdir /tmp/gitspork-downstream; \
		cd /tmp/gitspork-downstream; \
		git init; \
		cd $(PWD); \
	fi; \
	go run main.go integrate \
		--upstream-repo-url file://$(PWD) \
		--upstream-repo-subpath ./docs/examples/simple/upstream \
		--upstream-repo-version main \
		--downstream-repo-path /tmp/gitspork-downstream;

.PHONY: release
version ?= 
description ?=
release:
	@if [ -n "$$(git status -s)" ]; then echo "error: releasing only allowed on a clean working tree"; exit 1; fi; \
	if [ -z "$(version)" ]; then echo "error: please provide the 'version' for the release"; exit 1; fi; \
	if [ -z "$(description)" ]; then echo "error: please provide the 'description' for the release"; exit 1; fi; \
	if git ls-remote --tags "$$(git config --get remote.origin.url)" | grep -E '\trefs/tags/$(version)$$' &>/dev/null; then echo "error: git tag for version $(version) already exists in origin repo"; exit 1; fi; \
	if [[ -n "$$(git tag -l $(version))" ]]; then echo "error: git tag for version $(version) already exists locally"; exit 1; fi; \
	echo "releasing gitspork version: $(version), description: $(description)"; \
	git tag -a $(version) -m "$(description)"; \
	git push origin $(version); \
	goreleaser release --clean; \
	docker build -t rockholla/gitspork:$(version) .; \
	docker push rockholla/gitspork:$(version);


.PHONY: test
test:
	@go test -v ./...

.PHONY: ensure-local-test-downstream
ensure-local-test-downstream:
	@if [ ! -d /tmp/gitspork-downstream ]; then \
		mkdir /tmp/gitspork-downstream; \
		cd /tmp/gitspork-downstream; \
		git init; \
		cd $(PWD); \
	fi; \
	cp docs/examples/simple/templated-json-input-data.json /tmp/;

# A useful tests while developing locally
.PHONY: dev-test-integrate
dev-test-integrate: ensure-local-test-downstream
	@go run main.go integrate \
		--upstream-repo-url file://$(PWD) \
		--upstream-repo-subpath ./docs/examples/simple/upstream \
		--upstream-repo-version $$(git rev-parse --abbrev-ref HEAD) \
		--downstream-repo-path /tmp/gitspork-downstream;

.PHONY: dev-test-container-integrate
dev-test-container-integrate: ensure-local-test-downstream
	@docker rmi gitspork:local && docker system prune -f; \
	docker build -t gitspork:local .; \
	docker run -it --rm -v /tmp/gitspork-downstream:/downstream -v $(PWD):/upstream \
		gitspork:local \
			integrate \
				--upstream-repo-url file:///upstream \
				--upstream-repo-subpath ./docs/examples/simple/upstream \
				--upstream-repo-version $$(git rev-parse --abbrev-ref HEAD) \
				--downstream-repo-path /downstream;

.PHONY: dev-test-integrate-local
dev-test-integrate-local:
	@go run main.go integrate-local \
		--upstream-path ./docs/examples/local/upstream \
		--downstream-path ./docs/examples/local/downstream

.PHONY: release
version ?=
description ?=
latest ?= false
release:
	@if [ -n "$$(git status -s)" ]; then echo "error: releasing only allowed on a clean working tree"; exit 1; fi; \
	if [ -z "$(version)" ]; then echo "error: please provide the 'version' for the release"; exit 1; fi; \
	if [ -z "$(description)" ]; then echo "error: please provide the 'description' for the release"; exit 1; fi; \
	if git ls-remote --tags "$$(git config --get remote.origin.url)" | grep -E '\trefs/tags/$(version)$$' &>/dev/null; then echo "error: git tag for version $(version) already exists in origin repo"; exit 1; fi; \
	if [[ -n "$$(git tag -l $(version))" ]]; then echo "error: git tag for version $(version) already exists locally"; exit 1; fi; \
	echo "releasing gitspork version: $(version), description: $(description)"; \
	git tag -a $(version) -m "$(description)"; \
	git push origin $(version); \
	GITSPORK_VERSION=$(version) goreleaser release --clean; \
	docker build --build-arg "GITSPORK_VERSION=$(version)" -t rockholla/gitspork:$(version) .; \
	docker push rockholla/gitspork:$(version); \
	if [[ "$(latest)" == true ]]; then docker tag rockholla/gitspork:$(version) rockholla/gitspork:latest; docker push rockholla/gitspork:latest; fi;


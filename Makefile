.PHONY: test-unit
test-unit: ## Run unit tests
	@go test -v ./...

.PHONY: test-functional
test-functional: ## Run functional tests, compiles the tool and executes in real, functional scenarios using synthetic/dynamic repos
	@go test -tags functional -timeout 120s -v ./test/functional/...

.PHONY: test-functional-docker
test-functional-docker: ## Run tests specific to testing the containerized version of the tool
	@go test -tags functional_docker -timeout 300s -v ./test/functional/...

.PHONY: test-all
test-all: test test-functional test-functional-docker
test-all: ## Run all test suite types

# NOTE: recommended way to run all of this to support multi-arch image builds along w/ release artifacts through goreleaser:
#   1. Default macOS Docker desktop
#   2. enabled the containerd image store
#   3. `docker buildx use desktop-linux`
.PHONY: release
release: ## For releasing a version of the tool, will prompt for input for version, description etc.
	@./scripts/release.sh

## Provide help, explained at https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html.
.PHONY: help
help: ## display this help
	@grep -h -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort -u | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

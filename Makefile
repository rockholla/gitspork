.PHONY: schema
schema: ## Outputs the current working schema for the version of source
	@go run ./cmd/gitspork schema

.PHONY: build
build: ## Builds gitspork to dist/gitspork and builds Docker image tagged gitspork:local
	@mkdir -p dist dist/.docker-build
	@go build -o dist/gitspork ./cmd/gitspork
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/.docker-build/gitspork ./cmd/gitspork
	@cp Dockerfile dist/.docker-build/Dockerfile
	@docker build -t gitspork:local dist/.docker-build/
	@rm -rf dist/.docker-build

.PHONY: test-unit
test-unit: ## Run unit tests
	@go vet ./... && go test -v ./...

.PHONY: test-functional
test-functional: ## Run functional tests, compiles the tool and executes in real, functional scenarios using synthetic/dynamic repos
	@go test -tags functional -timeout 120s -v ./test/functional/...

.PHONY: test-functional-docker
test-functional-docker: ## Run tests specific to testing the containerized version of the tool
	@go test -tags functional_docker -timeout 300s -v ./test/functional/...

.PHONY: test-examples
test-examples: ## Run example scenario tests
	@go test -tags examples -timeout 120s -v ./test/examples/...

.PHONY: test-sdk
test-sdk: ## Run black-box SDK tests
	@go test -tags sdk -timeout 120s -v ./test/sdk/...

.PHONY: test-security-gate
test-security-gate: ## Run unit tests for the CI security gate script
	@./test/security-gate/run-tests.sh

.PHONY: test-all
test-all: test-unit test-security-gate test-functional test-functional-docker test-sdk ## Run all test suite types

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

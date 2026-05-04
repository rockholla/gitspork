.PHONY: test-unit
test-unit:
	@go test -v ./...

.PHONY: test-functional
test-functional:
	@go test -tags functional -timeout 120s -v ./test/functional/...

.PHONY: test-functional-docker
test-functional-docker:
	@go test -tags functional_docker -timeout 300s -v ./test/functional/...

# test-all runs unit + native functional tests. Docker tests are separate (test-functional-docker)
# because they require a running Docker daemon and take significantly longer.
# Use the ci target to run everything including Docker.
.PHONY: test
test: test-unit test-functional

.PHONY: ci
ci: test test-functional test-functional-docker

# NOTE: recommended way to run all of this to support multi-arch image builds along w/ release artifacts through goreleaser:
#   1. Default macOS Docker desktop
#   2. enabled the containerd image store
#   3. `docker buildx use desktop-linux`
.PHONY: release
release:
	@./scripts/release.sh

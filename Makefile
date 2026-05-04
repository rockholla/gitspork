.PHONY: test
test:
	@go test -v ./...

.PHONY: test-functional
test-functional:
	@go test -tags functional -timeout 120s -v ./test/functional/...

.PHONY: test-functional-docker
test-functional-docker:
	@go test -tags functional_docker -timeout 300s -v ./test/functional/...

.PHONY: test-all
test-all: test test-functional

.PHONY: ensure-local-test-downstream
ensure-local-test-downstream:
	@if [ ! -d /tmp/gitspork-downstream ]; then \
		mkdir /tmp/gitspork-downstream; \
		cd /tmp/gitspork-downstream; \
		git init; \
		cd $(PWD); \
	fi; \
	cp docs/examples/simple/templated-json-input-data.json /tmp/gitspork-downstream/;

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

.PHONY: dev-test-check-drift
dev-test-check-drift: ensure-local-test-downstream
	@echo "--- integrating downstream ---"; \
	go run main.go integrate \
		--upstream-repo-url file://$(PWD) \
		--upstream-repo-subpath ./docs/examples/simple/upstream \
		--upstream-repo-version $$(git rev-parse --abbrev-ref HEAD) \
		--downstream-repo-path /tmp/gitspork-downstream; \
	cd /tmp/gitspork-downstream && \
		git config user.email "gitspork@localhost" && \
		git config user.name "gitspork" && \
		git add -A && \
		git commit -m "post-integrate baseline" --allow-empty; \
	cd $(PWD); \
	echo "--- checking drift (expect: none) ---"; \
	cp docs/examples/simple/templated-json-input-data.json /tmp/gitspork-downstream/; \
	go run main.go check-drift --downstream-repo-path /tmp/gitspork-downstream; \
	if [ $$? -ne 0 ]; then echo "FAIL: expected no drift"; exit 1; fi; \
	echo "--- introducing drift ---"; \
	echo "# drifted" >> /tmp/gitspork-downstream/upstream-owned.mk; \
	cd /tmp/gitspork-downstream && git add -A && git commit -m "introduce drift"; \
	cd $(PWD); \
	echo "--- checking drift (expect: drift detected) ---"; \
	go run main.go check-drift --downstream-repo-path /tmp/gitspork-downstream --verbose; \
	if [ $$? -ne 1 ]; then echo "FAIL: expected drift detected (exit 1)"; exit 1; fi; \
	echo "--- drift detection scenarios passed ---"

.PHONY: dev-test-container-check-drift
dev-test-container-check-drift: ensure-local-test-downstream
	@docker rmi gitspork:local 2>/dev/null; docker system prune -f; \
	docker build -t gitspork:local .; \
	echo "--- integrating downstream (container) ---"; \
	docker run -it --rm -v /tmp/gitspork-downstream:/downstream -v $(PWD):/upstream \
		gitspork:local \
			integrate \
				--upstream-repo-url file:///upstream \
				--upstream-repo-subpath ./docs/examples/simple/upstream \
				--upstream-repo-version $$(git rev-parse --abbrev-ref HEAD) \
				--downstream-repo-path /downstream; \
	cd /tmp/gitspork-downstream && \
		git config user.email "gitspork@localhost" && \
		git config user.name "gitspork" && \
		git add -A && \
		git commit -m "post-integrate baseline" --allow-empty; \
	cd $(PWD); \
	echo "--- checking drift (expect: none, container) ---"; \
	docker run -it --rm -v /tmp/gitspork-downstream:/downstream -v $(PWD):/upstream \
		gitspork:local \
			check-drift \
				--downstream-repo-path /downstream; \
	if [ $$? -ne 0 ]; then echo "FAIL: expected no drift"; exit 1; fi; \
	echo "--- introducing drift ---"; \
	echo "# drifted" >> /tmp/gitspork-downstream/upstream-owned.mk; \
	cd /tmp/gitspork-downstream && git add -A && git commit -m "introduce drift"; \
	cd $(PWD); \
	echo "--- checking drift (expect: drift detected, container) ---"; \
	docker run -it --rm -v /tmp/gitspork-downstream:/downstream -v $(PWD):/upstream \
		gitspork:local \
			check-drift \
				--downstream-repo-path /downstream \
				--verbose; \
	if [ $$? -ne 1 ]; then echo "FAIL: expected drift detected (exit 1)"; exit 1; fi; \
	echo "--- container drift detection scenarios passed ---"

.PHONY: dev-test-integrate-local
dev-test-integrate-local:
	@go run main.go integrate-local \
		--upstream-path ./docs/examples/local/upstream \
		--downstream-path ./docs/examples/local/downstream

# NOTE: recommended way to run all of this to support multi-arch image builds along w/ release artifacts through goreleaser:
#   1. Default macOS Docker desktop
#   2. enabled the containerd image store
#   3. `docker buildx use desktop-linux`
.PHONY: release
release:
	@./scripts/release.sh

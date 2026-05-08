# default target
all: help

# include an overrides file, which sets up default values and allows user overrides
include overrides.mk

# Help target, prints usefule information
help:
	@echo
	@echo "The following targets are commonly used:"
	@echo
	@echo "action-help      - Displays instructions on how to run a single github workflow locally"
	@echo "actions          - Run all workflows locally, requires https://github.com/nektos/act"
	@echo "build            - Builds the code locally"
	@echo "check            - Runs the suite of code checking tools: lint, format, etc"
	@echo "clean            - Cleans the local build"
	@echo "docker           - Builds the code within a golang container and then creates the driver image"
	@echo "integration-test - Runs the integration tests. Requires access to an array"
	@echo "push             - Pushes the built container to a target registry"
	@echo "unit-test        - Runs the unit tests"
	@echo "vendor 			- Downloads a vendor list (local copy) of repositories required to compile the repo."
	@echo
	@make -s overrides-help

# Clean the build
clean:
	rm -f core/core_generated.go
	rm -f semver.mk
	rm -rf csm-common.mk
	rm -rf vendor
	go clean

generate:
	go generate
	go run core/semver/semver.go -f mk > semver.mk

vendor:
	GOPRIVATE=github.com go mod vendor

# Build the driver locally
build: generate
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -mod=vendor  

# Generates the docker container (but does not push)
docker: generate vendor
	make -f docker.mk docker

# Generates the docker container with no cache (but does not push)
docker-no-cache: generate vendor
	make -f docker.mk docker-no-cache

# Pushes container to the repository
push: docker
	make -f docker.mk push

# Windows or Linux; requires no hardware
unit-test: go-code-tester
	GITHUB_OUTPUT=/dev/null \
	./go-code-tester 90 "." "" "true" "" "" "./test"

# Linux only; populate env.sh with the hardware parameters
integration-test:
	( cd test/integration; sh run.sh TestIntegration )

check:
	@scripts/check.sh ./provider/ ./service/

go-code-tester:
	curl -o go-code-tester -L https://raw.githubusercontent.com/dell/common-github-actions/main/go-code-tester/entrypoint.sh \
	&& chmod +x go-code-tester

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
	@echo
	@make -s overrides-help

# Clean the build
clean:
	rm -f core/core_generated.go
	rm -f semver.mk
	go clean

# Dependencies
dependencies:
	go generate
	go run core/semver/semver.go -f mk >semver.mk

# Build the driver locally
build: dependencies
	(cd csi-md; make build)
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build

# Generates the docker container (but does not push)
docker: build
	docker build -t csi-vxflexos --network host .
	

# Generates the docker container with no cache (but does not push)
docker-no-cache: dependencies
	make -f docker.mk build-base-image docker-no-cache

# Pushes container to the repository
#push: docker
#	make -f docker.mk push

# Windows or Linux; requires no hardware
unit-test:
	( cd service; go clean -cache; CGO_ENABLED=1 GO111MODULE=on go test -v -race -coverprofile=c.out ./... )

# Linux only; populate env.sh with the hardware parameters
integration-test:
	( cd test/integration; sh run.sh )

check:
	@scripts/check.sh ./provider/ ./service/

.PHONY: actions
actions: ## Run all the github action checks that run on a pull_request creation
	act -l | grep -v ^Stage | grep pull_request | grep -v image_security_scan | awk '{print $$2}' | while read WF; do act pull_request --no-cache-server --platform ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest --job "$${WF}"; done

.PHONY: action-help
action-help: ## Echo instructions to run one specific workflow locally
	@echo "GitHub Workflows can be run locally with the following command:"
	@echo "act pull_request --no-cache-server --platform ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-latest --job <jobid>"
	@echo
	@echo "Where '<jobid>' is a Job ID returned by the command:"
	@echo "act -l"
	@echo
	@echo "NOTE: if act if not installed, it can be from https://github.com/nektos/act"


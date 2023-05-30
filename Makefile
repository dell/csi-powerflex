# default target
all: help

# include an overrides file, which sets up default values and allows user overrides
include overrides.mk

# Help target, prints usefule information
help:
	@echo
	@echo "The following targets are commonly used:"
	@echo
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
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build

# Generates the docker container (but does not push)
docker: dependencies
	make -f docker.mk build-base-image docker

# Pushes container to the repository
push: docker
	make -f docker.mk push

# Windows or Linux; requires no hardware
unit-test:
	( cd service; go clean -cache; CGO_ENABLED=1 GO111MODULE=on go test -v -race -coverprofile=c.out ./... )

# Linux only; populate env.sh with the hardware parameters
integration-test:
	( cd test/integration; sh run.sh )

check:
	@scripts/check.sh ./provider/ ./service/
	


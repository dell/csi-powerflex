# Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Dell Technologies, Dell and other trademarks are trademarks of Dell Inc.
# or its subsidiaries. Other trademarks may be trademarks of their respective 
# owners.

include images.mk

# default target
all: help

# This will be overridden during image build.
IMAGE_VERSION ?= 0.0.0
LDFLAGS = "-X main.ManifestSemver=$(IMAGE_VERSION)"

# Help target, prints usefule information
help:
	@echo
	@echo "The following targets are commonly used:"
	@echo
	@echo "build            - Builds the code locally"
	@echo "check            - Runs the suite of code checking tools: lint, format, etc"
	@echo "clean            - Cleans the local build"
	@echo "images           - Builds the code within a golang container and then creates the driver image"
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

# Build the driver locally
build: generate vendor
	CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -ldflags $(LDFLAGS) -mod=vendor

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
	git clone --depth 1 git@github.com:dell/actions.git temp-repo
	cp temp-repo/go-code-tester/entrypoint.sh ./go-code-tester
	chmod +x go-code-tester
	rm -rf temp-repo

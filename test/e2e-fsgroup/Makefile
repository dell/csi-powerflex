#
#
# Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
#
# Makefile for FSGroup e2e Tests

# Variables
GINKGO_BIN_PATH = /root/go/bin/ginkgo
GINKGO_INSTALL_CMD = go install github.com/onsi/ginkgo/v2/ginkgo
GINKGO_DEST_PATH = /usr/bin/ginkgo

# Default target to run all tasks
.PHONY: all
all: install-ginkgo run-tests

# Target to install Ginkgo v2, verify the installation, and move the binary
.PHONY: install-ginkgo
install-ginkgo:
	@echo "Installing Ginkgo v2..."
	$(GINKGO_INSTALL_CMD)
	@echo "Verifying Ginkgo installation..."
	$(GINKGO_BIN_PATH) version
	@echo "Moving Ginkgo binary to /usr/bin..."
	sudo mv $(GINKGO_BIN_PATH) $(GINKGO_DEST_PATH)

# Target to run FSGroup e2e tests
.PHONY: run-tests
run-tests:
	@echo "Running FSGroup e2e tests..."
	./run.sh

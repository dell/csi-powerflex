.PHONY: all
all: help

help:
	@echo
	@echo "The following targets are commonly used:"
	@echo
	@echo "integration			- Run integration tests"
	@echo "zone-integration		- Run zone-integration tests"
	@echo

.PHONY: integration
integration: 
	cd integration; ./run.sh TestIntegration

.PHONY: zone-integration
zone-integration:
	cd integration; ./run.sh TestZoneIntegration

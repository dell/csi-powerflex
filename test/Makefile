.PHONY: all
all: help

help:
	@echo
	@echo "The following targets are commonly used:"
	@echo
	@echo "integration			- Run integration tests"
	@echo "zone-integration		- Run zone-integration tests"
	@echo "zone-e2e				- Run zone-e2e tests"
	@echo

.PHONY: integration
integration:
	cd integration; ./run.sh TestIntegration

.PHONY: zone-integration
zone-integration:
	cd integration; ./run.sh TestZoneIntegration

.PHONY: zone-e2e
zone-e2e:
	go test -tags=integration -v -count=1 -timeout 1h -run '^TestZoneVolumes$$' ./e2e

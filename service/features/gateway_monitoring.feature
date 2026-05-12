Feature: PowerFlex Gateway Monitoring
  As a storage administrator
  I want the CSI PowerFlex driver to monitor gateway health and expose Prometheus metrics
  So that I can observe, alert on, and respond to gateway availability changes

  # BDD-001 — Scenario 1: Gateway monitoring enabled and healthy → /metrics shows up=1
  Scenario: Gateway monitoring enabled and healthy gateway reports up
    Given gateway monitoring is enabled with poll interval "50ms"
    And a mock gateway client for system "sys1" that returns version "4.0.0"
    And the gateway "sys1" has IP address "10.0.0.1"
    When I start the metrics server on a random port
    And I start the gateway monitor
    And I wait "200ms" for polling to complete
    Then the metrics endpoint returns HTTP 200
    And the metrics body contains "powerflex_gateway_up"
    And the metrics body contains label "system_id" with value "sys1"
    And the metrics body contains label "gateway_endpoint" with value "10.0.0.1"
    And the metric "powerflex_gateway_up" for system "sys1" equals 1

  # BDD-001 — Scenario 2: Gateway goes down → up=0 and error counter increments
  Scenario: Gateway goes down and metrics reflect the failure
    Given gateway monitoring is enabled with poll interval "50ms"
    And a mock gateway client for system "sys1" that returns error "connection refused"
    And the gateway "sys1" has IP address "10.0.0.1"
    When I start the metrics server on a random port
    And I start the gateway monitor
    And I wait "200ms" for polling to complete
    Then the metrics endpoint returns HTTP 200
    And the metric "powerflex_gateway_up" for system "sys1" equals 0
    And the metrics body contains "powerflex_gateway_errors_total"
    And the metrics body contains "powerflex_gateway_consecutive_failures"

  # BDD-001 — Scenario 3: Leader context cancelled → polling stops
  Scenario: Leader context cancellation stops polling goroutines
    Given gateway monitoring is enabled with poll interval "30ms"
    And a mock gateway client for system "sys1" that returns version "4.0.0"
    And the gateway "sys1" has IP address "10.0.0.1"
    When I start the metrics server on a random port
    And I start the gateway monitor with a cancellable context
    And I wait "120ms" for polling to complete
    And the mock client for "sys1" has received at least 1 call
    And I cancel the gateway monitor context
    And I record the current call count for "sys1"
    And I wait "150ms" for goroutines to drain
    Then no additional calls are made to the mock client for "sys1"
    And the metrics server still returns HTTP 200

  # BDD-001 — Scenario 4: Feature disabled → no metrics endpoint
  Scenario: Gateway monitoring disabled means no metrics are exposed
    Given gateway monitoring is disabled
    When I do not start the gateway monitor
    Then no metrics server is running
    And no HTTP listener exists on the metrics port

  # BDD-001 — Scenario 5: Credentials never appear in metric labels
  Scenario: Metric labels must not contain credentials or secrets
    Given gateway monitoring is enabled with poll interval "50ms"
    And a mock gateway client for system "sys1" that returns version "4.0.0"
    And the gateway "sys1" has IP address "10.0.0.1"
    And the configuration labels contain sensitive key "password" with value "secret123"
    When I attempt to start the gateway monitor with credential labels
    Then either an error is returned or the credentials are stripped from labels
    And the metrics body does not contain "password"
    And the metrics body does not contain "secret123"
    And the metrics body does not contain "token"

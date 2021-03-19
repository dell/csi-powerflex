Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test service methods
  So that they are known to work

  @pmon
  Scenario: Call ValidateConnectivity
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "podmon1"
    Then a valid CreateVolumeResponse is returned
    And I call ValidateConnectivity
    Then no error was received

  @pmon
  Scenario: Call ValidateConnectivity with node probe error
    Given a VxFlexOS service
    And I induce error "node-probe"
    And I call ValidateConnectivity
    Then the error contains "NodeID is invalid"

  @pmon
  Scenario: Call ValidateConnectivity with no Volume no Node
    Given a VxFlexOS service
    And I induce error "no-volume-no-nodeId"
    And I call ValidateConnectivity
    Then the error contains "ValidateVolumeHostConnectivity is implemented"

  @pmon
  Scenario: Call ValidateConnectivity with no Node
    Given a VxFlexOS service
    And I call CreateVolume "podmon1"
    And I induce error "no-nodeId"
    And I call ValidateConnectivity
    Then the error contains "The NodeID is a required field"

  @pmon
  Scenario: Call ValidateConnectivity with no System
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "podmon1"
    And I induce error "no-system"
    And I call ValidateConnectivity
    Then the error contains "NodeID is invalid"

  @pmon
  Scenario: Call ValidateConnectivity with contoller probe error
    Given a VxFlexOS service
    And I induce error "controller-probe"
    And I call ValidateConnectivity
    Then the error contains "NodeID is invalid"

  @pmon
  Scenario: Call ValidateConnectivity with no System
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "podmon1"
    And I induce error "no-sdc"
    And I call ValidateConnectivity
    Then the error contains "NodeID is invalid"

  @pmon
  Scenario: Call ValidateConnectivity with volume error
    Given a VxFlexOS service
    And I induce error "volume-error"
    And I call ValidateConnectivity
    Then the ValidateConnectivity response message contains "Could not retrieve volume"

  @pmon
  Scenario: Call ValidateConnectivity with volume statistics error
    Given a VxFlexOS service
    And I call CreateVolume "podmon1" 
    And I induce error "no-volume-statistics"
    And I call ValidateConnectivity
    Then the error contains "Could not retrieve volume statistics"



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
    When I call Probe
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
    When I call Probe
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

  @vg
  Scenario Outline: Call CreateVolumeSnapshotGroup with errors
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I induce error <error>
    And I call CreateVolumeSnapshotGroup
    Then the error contains <errorMsg>
    And a valid CreateVolumeSnapshotGroup response is returned 

Examples:
      | error                       | errorMsg                          |
      | "none"                      | "none"                            |
      | "VolIDListEmptyError"       | "SourceVolumeIDs cannot be empty" |
      | "CreateVGSAcrossTwoArrays"  | "should be on the same system"    |
      | "CreateVGSNameTooLongError" | "longer than 27 character max"    |
      | "SIOGatewayVolumeNotFound"  | "failure checking source"         |
      | "CreateVGSLegacyVol"        | "none"                            |
      | "CreateSnapshotError"       | "Failed to create group"          |
      | "NoSysNameError"            | "systemID is not found"           | 
     
  @vg
  Scenario: I call CreateVolumeSnapshotGroup with legacy vol conflict
    Given a VxFlexOS service
    #When I call Probe
    #And I induce error "LegacyVolumeConflictError"
    And a valid volume
    When I call Probe
    And I induce error "LegacyVolumeConflictError"
    And I call CreateVolumeSnapshotGroup
    Then the error contains "Expecting this volume id only on default system"

  @vg
  Scenario: Call DeleteVolumeSnapshotGroup
    Given a VxFlexOS service
    When I call Probe
    And I call DeleteVolumeSnapshotGroup
    Then the error contains "none"
  
  @vg
  Scenario: Snapshot a block volume consistency group with wrong system
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I induce error "WrongSystemError"
    And I call CreateSnapshot "snap1"
    Then the error contains "needs to be on the same system"
 
  @vg
  Scenario: CheckCreationTime with non consistent times
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolumeSnapshotGroup
    Then the error contains "none"
    And I induce error "CreateVGSBadTimeError"
    And I call CheckCreationTime
    Then the error contains "All snapshot creation times should be equal"

 @vg
 Scenario: CreateVolumeSnapshotGroup idempotent 
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolumeSnapshotGroup
    And I call CreateVolumeSnapshotGroup
    Then the error contains "none"

 @vg
 Scenario: CreateVolumeSnapshotGroup idempotent; when criteria 1 fails
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolumeSnapshotGroup
    And I call CreateVolume "vol4"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolumeSnapshotGroup
    Then the error contains "Some snapshots exist on array, while others need to be created."

@vg
Scenario: Call CreateVolumeGroupSnapshot idempotent; criteria 3 fails
  Given a VxFlexOS service
  When I call Probe
  And I call CreateVolume "vol1"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolume "vol2"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolume "vol3"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolumeSnapshotGroup
  And remove a volume from VolumeGroupSnapshotRequest
  And I call CreateVolumeSnapshotGroup
  Then the error contains "contains more snapshots"



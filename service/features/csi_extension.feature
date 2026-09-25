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
  Scenario: Call ValidateConnectivity for NVMeTCP
    Given a VxFlexOS service
    When I call Probe
    And I set protocol to "NVMeTCP"
    And I call CreateVolume "podmon1"
    Then a valid CreateVolumeResponse is returned
    And I call ValidateConnectivity
    Then no error was received

  @pmon
    Scenario: Call ValidateConnectivity for GenType EC
      Given a VxFlexOS service
      When I call Probe
      And I call CreateVolume "podmon1"
      Then a valid CreateVolumeResponse is returned
      And I set Platform Info "5.0" "EC" "5.0" "EC"
      And I call ValidateConnectivity
      Then no error was received
      Then I reset the Platform Info

  @pmon
  Scenario: Call ValidateConnectivity with node probe error
    Given a VxFlexOS service
    When I call Probe
    And I induce error "node-probe"
    And I call ValidateConnectivity
    Then the error contains "error getting host ID"

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
    Then the error contains "error getting host ID"

  @pmon
  Scenario: Call ValidateConnectivity with contoller probe error
    Given a VxFlexOS service
    When I call Probe
    And I induce error "controller-probe"
    And I call ValidateConnectivity
    Then the error contains "error getting host ID"

  @pmon
  Scenario: Call ValidateConnectivity with no System
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "podmon1"
    And I induce error "no-sdc"
    And I call ValidateConnectivity
    Then the error contains "error getting host ID"

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
      | "VolIDListEmptyError"       | "cannot be empty"                 |
      | "CreateVGSAcrossTwoArrays"  | "on the same system"              |
      | "CreateVGSNameTooLongError" | "none"                            |
      | "SIOGatewayVolumeNotFound"  | "not found"                       |
      | "CreateVGSLegacyVol"        | "none"                            |
      | "CreateSnapshotError"       | "failed to create group snapshot"  |
      | "NoSysNameError"            | "systemID is not found"           | 
     
  @vg
  Scenario: I call CreateVolumeSnapshotGroup with legacy vol conflict
    Given a VxFlexOS service
    And I induce error "LegacyVolumeConflictError"
    And a valid volume
    When I call Probe
    And I call CreateVolumeSnapshotGroup
    Then the error contains "expecting this volume id only on default system"

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
    Then the error contains "some snapshots exist on array while others do not"

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
  Then the error contains "contains"

@vg
Scenario: Call DeleteVolumeGroupSnapshot successfully
  Given a VxFlexOS service
  When I call Probe
  And I call CreateVolume "vol1"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolume "vol2"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolumeSnapshotGroup
  And a valid CreateVolumeSnapshotGroup response is returned
  And I call DeleteVolumeGroupSnapshot
  Then the error contains "none"

@vg
Scenario: Call DeleteVolumeGroupSnapshot with empty ID
  Given a VxFlexOS service
  When I call Probe
  And I call DeleteVolumeGroupSnapshot with empty ID
  Then the error contains "group_snapshot_id is required"

@vg
Scenario: Call DeleteVolumeGroupSnapshot with invalid ID format
  Given a VxFlexOS service
  When I call Probe
  And I call DeleteVolumeGroupSnapshot with invalid ID
  Then the error contains "none"

@vg
Scenario: Call GetVolumeGroupSnapshot successfully
  Given a VxFlexOS service
  When I call Probe
  And I call CreateVolume "vol1"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolume "vol2"
  And a valid CreateVolumeResponse is returned
  And I call CreateVolumeSnapshotGroup
  And a valid CreateVolumeSnapshotGroup response is returned
  And I call GetVolumeGroupSnapshot
  Then the error contains "none"

@vg
Scenario: Call GetVolumeGroupSnapshot with empty ID
  Given a VxFlexOS service
  When I call Probe
  And I call GetVolumeGroupSnapshot with empty ID
  Then the error contains "group_snapshot_id is required"

@vg
Scenario: Call GetVolumeGroupSnapshot with invalid ID format
  Given a VxFlexOS service
  When I call Probe
  And I call GetVolumeGroupSnapshot with invalid ID
  Then the error contains "not found"

@vg
Scenario: Call GetVolumeGroupSnapshot with non-existent group
  Given a VxFlexOS service
  When I call Probe
  And I call GetVolumeGroupSnapshot with non-existent group
  Then the error contains "not found"

Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
   
   Scenario: Craete volume, clone volume, delete original volume, delete new volume
    Given a VxFlexOS service
    And I set another systemID <id>
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And I call CloneVolume
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And I call ListSnapshot
    And a valid ListSnapshotResponse is returned
    And when I call DeleteVolume
    And there are no errors
    And when I call DeleteAllVolumes
    And there are no errors
    And I call ListVolume
    Examples:
      | id              |   
      | "defaultSystem" |
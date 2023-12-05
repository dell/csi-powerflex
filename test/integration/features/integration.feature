Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
  
  Scenario: NFS Create volume, create snapshot, delete volume
    Given a VxFlexOS service
    And a basic nfs volume request "nfsvolume1" "8"
    When I call CreateVolume
    And I call CreateSnapshotForFS
    And there are no errors
    And I call ListFileSystemSnapshot
    And there are no errors
    And I call DeleteSnapshotForFS
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
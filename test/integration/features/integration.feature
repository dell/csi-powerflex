Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Create and delete basic volume
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    When I call ListVolume
    Then a valid ListVolumeResponse is returned
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Idempotent create and delete basic volume
    Given a VxFlexOS service
    And a basic block volume request "integration2" "8"
    When I call CreateVolume
    And I call CreateVolume
    And when I call DeleteVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete mount volume
    Given a VxFlexOS service
    And a mount volume request "integration5"
    When I call CreateVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create publish, unpublish, and delete basic volume
    Given a VxFlexOS service
    And a basic block volume request "integration5" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create volume, create snapshot, create volume from snapshot, delete original volume, delete new volume
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And I call CreateSnapshot
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And I call ListSnapshot
    And a valid ListSnapshotResponse is returned
    And I call CreateVolumeFromSnapshot
    Then there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    And there are no errors
    And when I call DeleteAllVolumes
    And there are no errors
    And I call ListVolume

  Scenario: Create volume, create snapshot, create many volumes from snap, delete original volume, delete new volumes
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And there are no errors
    And I call CreateSnapshot
    And there are no errors
    And I call CreateManyVolumesFromSnapshot
    Then the error message should contain "There are too many snapshots in the VTree"
    And I call DeleteSnapshot
    And when I call DeleteVolume
    And when I call DeleteAllVolumes
    

  Scenario: Create volume, idempotent create snapshot, delete volume
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And I call CreateSnapshot
    And there are no errors
    And I call CreateSnapshot
    And there are no errors
    And I call DeleteSnapshot
    And there are no errors
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create multiple volumes, create snapshot of consistency group, delete volumes
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And a basic block volume request "integration2" "8"
    And I call CreateVolume
    And a basic block volume request "integration3" "8"
    And I call CreateVolume
    And I call CreateSnapshotConsistencyGroup
    And there are no errors
    Then I call DeleteSnapshot
    And there are no errors
    And when I call DeleteAllVolumes
    And there are no errors

  Scenario Outline: Create publish, node-publish, node-unpublish, unpublish, and delete basic volume
    Given a VxFlexOS service
    And a capability with voltype <voltype> access <access> fstype <fstype>
    And a volume request "integration5" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And when I call NodePublishVolume "SDC_GUID"
    And verify published volume with voltype <voltype> access <access> fstype <fstype>
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And when I call DeleteVolume
    Then the error message should contain <errormsg>

    Examples:
    | voltype      | access                         | fstype     | errormsg                                     |
    | "mount"      | "single-writer"                | "xfs"      | "none"                                       |
    | "mount"      | "single-writer"                | "ext4"     | "none"                                       |
    | "mount"      | "multi-writer"                 | "ext4"     | "multi-writer not allowed"                   |
    | "block"      | "single-writer"                | "none"     | "none"                                       |
    | "block"      | "multi-writer"                 | "none"     | "none"                                       |
    | "block"      | "single-writer"                | "none"     | "none"                                       |
    


   
  Scenario: Create volume with access mode read only many
   Given a VxFlexOS service
   And a capability with voltype "mount" access "single-writer" fstype "xfs"
   And a volume request "multi-reader-test" "8"
   When I call CreateVolume
   And there are no errors
   And when I call PublishVolume "SDC_GUID"
   And when I call NodePublishVolume "SDC_GUID"
   And when I call NodeUnpublishVolume "SDC_GUID"
   And when I call UnpublishVolume "SDC_GUID"
   And a capability with voltype "mount" access "multi-reader" fstype "xfs"
   And when I call PublishVolume "SDC_GUID"
   And when I call NodePublishVolumeWithPoint "SDC_GUID" "temp1" 
   And when I call NodePublishVolumeWithPoint "SDC_GUID" "temp2" 
   And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "temp1"
   And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "temp2"
   And when I call UnpublishVolume "SDC_GUID"
   And when I call DeleteVolume
   Then there are no errors


  Scenario: Create publish, unpublish, and delete basic volume
    Given a VxFlexOS service
    And a basic block volume request "integration5" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Multi-host create publish, unpublish, and delete basic volume
    Given a VxFlexOS service
    And a basic block volume request "integration6" "8"
    And access type is "multi-writer"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call PublishVolume "ALT_GUID"
    And there are no errors
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume "ALT_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic 100000G volume
    Given a VxFlexOS service
    And max retries 1
    And a basic block volume request "integration4" "100000"
    When I call CreateVolume
    And when I call DeleteVolume
    Then the error message should contain "Requested volume size exceeds the volume allocation limit"

  Scenario: Create and delete basic 96G volume
    Given a VxFlexOS service
    And max retries 10
    And a basic block volume request "integration3" "96"
    When I call CreateVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario Outline: Scalability test to create volumes, publish, node publish, node unpublish, unpublish, delete volumes in parallel
    Given a VxFlexOS service
    When I create <numberOfVolumes> volumes in parallel
    And there are no errors
    And I publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And when I delete <numberOfVolumes> volumes in parallel
    Then there are no errors

    Examples:
    | numberOfVolumes |
    | 1               |
    | 2               |
    | 5               |
    | 10              |
    | 20              |
    | 50              |
    | 100             |
    | 200             |

  Scenario Outline: Idempotent create volumes, publish, node publish, node unpublish, unpublish, delete volumes in parallel
    Given a VxFlexOS service
    When I create <numberOfVolumes> volumes in parallel
    And there are no errors
    When I create <numberOfVolumes> volumes in parallel
    And there are no errors
    And I publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And when I delete <numberOfVolumes> volumes in parallel
    And there are no errors
    And when I delete <numberOfVolumes> volumes in parallel
    Then there are no errors

    Examples:
    | numberOfVolumes |
    | 1               |
    | 10               |
    | 20               |

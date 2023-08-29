Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario Outline: Create publish, node-publish, node-unpublish, unpublish, and delete basic volume
    Given a VxFlexOS service
    And a capability with voltype <voltype> access <access> fstype <fstype>
    And a volume request "podmon1" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And when I call NodePublishVolumeWithPoint "SDC_GUID" "/tmp/podmondev1"
    And there are no errors
    And I read write data to volume "/tmp/podmondev1"
    And when I call Validate Volume Host connectivity
    Then there are no errors
    And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "/tmp/podmondev1"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    Examples:
      | voltype | access                      | fstype | errormsg |
      | "mount" | "single-writer"             | "xfs"  | "none"   |
      | "mount" | "single-node-single-writer" | "xfs"  | "none"   |
      | "mount" | "single-node-multi-writer"  | "xfs"  | "none"   |


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

  #note: only run if secret has systemID
  Scenario: Create, publish, unpublish, and delete basic vol, but sc has name, and secret has id
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And a volume request "alt_system_id_integration7" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    Examples:
      | access                      |
      | "single-writer"             |
      | "single-node-single-writer" |
      | "single-node-multi-writer"  |

  Scenario Outline: Create, publish, unpublish, and delete basic vol, using systemName. Second run: sc has ID, but secret has name
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And I set another systemName "altSystem"
    And a volume request <name> "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

    Examples:
      | name                         |
      | "integration7"               |
      | "alt_system_id_integration8" |

  Scenario: Create, publish, unpublish, and delete basic vol, change name of array and specify wrong allSystemNames , this will pass if volume because handle has id
    Given a VxFlexOS service
    And I set another systemID "altSystem"
    And Set System Name As "1235e15806d1ec0f-pflex-system"
    And Set Bad AllSystemNames
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And a volume request "integration9" "8"
    When I call CreateVolume
    And Set System Name As "1235e15806d1ec0f_pflex_system"
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume


  Scenario: Create, publish, unpublish, and delete basic vol, change name of array and specify allSystemNames
    Given a VxFlexOS service
    And I set another systemID "altSystem"
    And Set System Name As "1235e15806d1ec0f-pflex-system"
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And a volume request "integration8" "8"
    When I call CreateVolume
    And Set System Name As "1235e15806d1ec0f_pflex_system"
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  @long
  Scenario Outline: Create volume, create snapshot, delete snapshot, delete volume for multiple sizes
    Given a VxFlexOS service
    And a capability with voltype "block" access "single-writer" fstype "xfs"
    And a basic block volume request "integration1" <size>
    When I call CreateVolume
    And when I call PublishVolume "SDC_GUID"
    And when I call NodePublishVolume "SDC_GUID"
    And verify published volume with voltype "block" access "single-writer" fstype "xfs"
    And when I call NodePublishVolume "SDC_GUID"
    And I write block data
    And I call CreateSnapshot
    And there are no errors
    And I call DeleteSnapshot
    And there are no errors
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And when I call DeleteVolume
    And there are no errors
    And when I call DeleteAllVolumes
    And there are no errors

    Examples:
      | size |
      | "8"  |
      | "16" |
      | "32" |
      | "64" |

  Scenario: Create volume, create snapshot, create volume from snapshot, delete original volume, delete new volume
    Given a VxFlexOS service
    And I set another systemID "altSystem"
    And a basic block volume request "ss1" "8"
    When I call CreateVolume
    And I call CreateSnapshot
    And there are no errors
    And I call ListSnapshot
    And expect Error ListSnapshotResponse
    And I call DeleteSnapshot
    And there are no errors
    And when I call DeleteVolume
    And there are no errors


  Scenario: Create volume, create snapshot, create volume from snapshot, delete original volume, delete new volume
    Given a VxFlexOS service
    And I set another systemID <id>
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And I call CreateSnapshot
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And I call ListSnapshot For Snap
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
    Examples:
      | id              |
      | "altSystem"     |
      | "defaultSystem" |

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
      | "altSystem"     |
      | "defaultSystem" |

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

  Scenario: Craete volume, clone volume, clone many volumes, delete original volume, delete new volumes
    Given a VxFlexOS service
    And a basic block volume request "integration1" "8"
    When I call CreateVolume
    And I call CloneVolume
    And there are no errors
    And I call CloneManyVolumes
    Then the error message should contain "There are too many snapshots in the VTree"
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
    And I call ListSnapshot
    And a valid ListSnapshotResponse is returned
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
      | voltype | access                      | fstype | errormsg                   |
      | "mount" | "single-writer"             | "xfs"  | "none"                     |
      | "mount" | "single-writer"             | "ext4" | "none"                     |
      | "mount" | "single-node-single-writer" | "xfs"  | "none"                     |
      | "mount" | "single-node-single-writer" | "ext4" | "none"                     |
      | "mount" | "multi-writer"              | "ext4" | "multi-writer not allowed" |
      | "block" | "single-writer"             | "none" | "none"                     |
      | "block" | "single-node-single-writer" | "none" | "none"                     |
      | "block" | "multi-writer"              | "none" | "none"                     |
      | "block" | "single-writer"             | "none" | "none"                     |
      | "block" | "single-node-single-writer" | "none" | "none"                     |

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

  Scenario: Create block volume with access mode read write many
    Given a VxFlexOS service
    And a capability with voltype "block" access "multi-writer" fstype ""
    And a volume request "block-multi-writer-test" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And when I call PublishVolume "ALT_GUID"
    And when I call NodePublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev1"
    And there are no errors
    And when I call NodePublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev2"
    And there are no errors
    And when I call NodePublishVolume "ALT_GUID"
    And there are no errors
    And when I call NodeUnpublishVolume "ALT_GUID"
    And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev1"
    And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev2"
    And when I call UnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "ALT_GUID"
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
    And I set another systemID <id>
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
      | id              | numberOfVolumes |
      | "altSystem"     | 5               |
      | "defaultSystem" | 5               |

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
      | 10              |

  Scenario: Expand Volume Mount
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And a volume request "integration30" "16"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And there are no errors
    And when I call ExpandVolume to "20"
    And there are no errors
    And when I call NodeExpandVolume
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Expand Volume Block
    Given a VxFlexOS service
    And a capability with voltype "block" access "single-writer" fstype "none"
    And a volume request "integration33" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And there are no errors
    And when I call ExpandVolume to "10"
    And there are no errors
    And when I call NodeExpandVolume
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create and delete basic nfs volume
    Given a VxFlexOS service
    And a basic nfs volume request "nfsvolume1" "8"
    When I call CreateVolume
    When I call ListVolume
    Then a valid ListVolumeResponse is returned
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Idempotent create and delete basic nfs volume
    Given a VxFlexOS service
    And a basic nfs volume request "nfsvolume2" "8"
    When I call CreateVolume
    And I call CreateVolume
    And when I call DeleteVolume
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create basic nfs volume with tree quota enabled with empty path value, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "" softlimit "20" graceperiod "86400"
    When I call CreateVolume
    Then the error message should contain "path not set for volume"

  
  Scenario: Create basic nfs volume with tree quota enabled with empty softlimit value, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "" graceperiod "86400"
    When I call CreateVolume
    Then the error message should contain "softLimit not set for volume"

  
  Scenario: Create basic nfs volume with tree quota enabled with empty graceperiod value
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "20" graceperiod ""
    When I call CreateVolume
    When I call ListVolume
    Then a valid ListVolumeResponse is returned
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create basic nfs volume with tree quota enabled with invalid softlimit, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "abc" graceperiod "86400"
    When I call CreateVolume
    Then the error message should contain "requested softLimit: abc is not numeric for volume"

  Scenario: Create basic nfs volume with tree quota enabled with invalid graceperiod, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "20" graceperiod "abc"
    When I call CreateVolume
    Then the error message should contain "requested gracePeriod: abc is not numeric for volume"

  Scenario: Create basic nfs volume with tree quota enabled with unlimited softlimit, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "0" graceperiod "86400"
    When I call CreateVolume
    Then the error message should contain "requested softLimit: 0 perc, i.e. default value which is greater than hardlimit, i.e. volume size"

  Scenario: Create basic nfs volume with tree quota enabled with unlimited graceperiod
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "20" graceperiod "-1"
    When I call CreateVolume
    When I call ListVolume
    Then a valid ListVolumeResponse is returned
    And when I call DeleteVolume
    Then there are no errors

  Scenario: Create basic nfs volume with tree quota enabled with softlimit greater than size, error
    Given a VxFlexOS service
    And a basic nfs volume request with quota enabled volname "nfsvolume1" volsize "8" path "/fs" softlimit "200" graceperiod "86400"
    When I call CreateVolume
    Then the error message should contain "requested softLimit: 200 perc is greater than volume size"
  
  Scenario: Expand Nfs Volume with tree quota enabled
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "nfs"
    And a basic nfs volume request with quota enabled volname "vol-quota" volsize "10" path "/nfs-quotakk" softlimit "80" graceperiod "86400"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "15"
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  
  Scenario: Expand Nfs Volume with tree quota enabled given invalid volume size for exapnd volume
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "nfs"
    And a basic nfs volume request with quota enabled volname "vol-quota123" volsize "10" path "/nfs-quotakk" softlimit "80" graceperiod "86400"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "15000"
    Then the error message should contain <errormsg>
    Examples:
    | errormsg    |
    | "422 Unprocessable Entity" |   
    

  Scenario: Expand Nfs Volume with tree quota disabled
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "nfs"
    And a basic nfs volume request with quota enabled volname "vol-quota" volsize "10" path "/nfs-quota" softlimit "80" graceperiod "86400"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "15"
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors    

  Scenario: Create and delete 100000G NFS volume
    Given a VxFlexOS service
    And max retries 1
    And a basic nfs volume request "nfsvolume2" "100000"
    When I call CreateVolume
    And when I call DeleteVolume
    Then the error message should contain "Unprocessable Entity"

  Scenario: Create a NFS volume with wrong NasName
    Given a VxFlexOS service
    And a basic nfs volume request with wrong nasname "nfsvolume3" "8"
    When I call CreateVolume
    Then the error message should contain <errormsg>
    Examples:
      | errormsg    |
      | "couldn't find given NAS server by name" |

  Scenario Outline: Create publish, node-publish, node-unpublish, unpublish, and delete nfs volume
    Given a VxFlexOS service
    And a nfs capability with voltype <voltype> access <access> fstype <fstype>
    And a nfs volume request "nfsinttestvol" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodeUnpublishVolume for nfs "SDC_GUID"
    And when I call UnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
    Examples:
      | voltype | access          | fstype | errormsg |
      | "mount" | "single-writer" | "nfs"  | "none"   | 

  Scenario: Expand Nfs Volume
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "nfs"
    And a nfs volume request "nfsinttestvol2" "16"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "20"
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

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

  Scenario: NFS Create volume, idempotent create snapshot, delete volume
    Given a VxFlexOS service
    And a basic nfs volume request "nfsvolume1" "8"
    When I call CreateVolume
    And I call CreateSnapshotForFS
    And there are no errors
    And I call CreateSnapshotForFS
    And there are no errors
    And I call ListFileSystemSnapshot
    And there are no errors
    And I call DeleteSnapshotForFS
    And there are no errors
    And I call DeleteSnapshotForFS
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

  Scenario Outline: Publish and Unpublish Ephemeral Volume
    Given a VxFlexOS service
    And a capability with voltype "mount" access <access> fstype <fstype>
    And I call EthemeralNodePublishVolume with ID <id> and size <size>
    And when I call NodeUnpublishVolume "SDC_GUID"
    Then the error message should contain <errormsg>
Examples:
  |  id           | size      |  access         | fstype | errormsg                                             | 
  | "123456789"   | "8Gi"     |"single-writer"  | "xfs"  | "none"                                               | 
  | "123456789"   | "8Gi"     |"single-writer"  | "ext4" | "none"                                               |
  | "123456789"   | "8Gi"     | "multi-writer"  | "ext4" | "inline ephemeral controller publish failed"         | 
  | ""            | "8Gi"     | "single-writer" | "ext4" | "InvalidArgument desc = required: VolumeID"          |
  | "123456789"   | "8Gi"     | "single-writer" | "ext1" | "inline ephemeral node publish failed"               |
  | "123456789"   | " Gi"     |"single-writer"  | "ext4" | "inline ephemeral parse size failed"                 |

Scenario: Call CreateVolumeGroupSnapshot
  Given a VxFlexOS service
  And a basic block volume request "integration1" "8"
  When I call CreateVolume
  And a basic block volume request "integration2" "8"
  And I call CreateVolume
  And a basic block volume request "integration3" "8"
  And I call CreateVolume
  When I call CreateVolumeGroupSnapshot
  And I call DeleteVGS
  And when I call DeleteAllVolumes
  Then the error message should contain "none"

Scenario: Call CreateVolumeGroupSnapshot idempotent 
  Given a VxFlexOS service
  And a basic block volume request "integration1" "8"
  When I call CreateVolume
  And a basic block volume request "integration2" "8"
  And I call CreateVolume
  And a basic block volume request "integration3" "8"
  And I call CreateVolume
  When I call CreateVolumeGroupSnapshot
  When I call CreateVolumeGroupSnapshot
  And I call DeleteVGS
  And when I call DeleteAllVolumes
  Then the error message should contain "none"

@vg
Scenario: Call CreateVolumeGroupSnapshot idempotent; criteria 1 fails
  Given a VxFlexOS service
  And a basic block volume request "integration1" "8"
  When I call CreateVolume
  And a basic block volume request "integration2" "8"
  And I call CreateVolume
  And a basic block volume request "integration3" "8"
  And I call CreateVolume
  When I call CreateVolumeGroupSnapshot
  And a basic block volume request "integration4" "8"
  And I call CreateVolume
  When I call CreateVolumeGroupSnapshot
  And I call DeleteVGS
  And when I call DeleteAllVolumes
  Then the error message should contain "Some snapshots exist on array, while others need to be created"

#X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE must be set to "false" in env.sh for this test
#Scenario: Call CreateVolumeGroupSnapshot idempotent; criteria 2 fails
# Given a VxFlexOS service
#  And a basic block volume request "integration1" "8"
#  When I call CreateVolume
# And a basic block volume request "integration2" "8"
# And I call CreateVolume
#  And a basic block volume request "integration3" "8"
#  And I call CreateVolume
#  When I call CreateVolumeGroupSnapshot
#  And I call split VolumeGroupSnapshot
#  When I call CreateVolumeGroupSnapshot
#  And I call DeleteVGS
#  And when I call DeleteAllVolumes
#  Then the error message should contain "Idempotent snapshots belong to different consistency groups on array"

@vg
Scenario: Call CreateVolumeGroupSnapshot idempotent; criteria 3 fails
  Given a VxFlexOS service
  And a basic block volume request "integration1" "8"
  When I call CreateVolume
  And a basic block volume request "integration2" "8"
  And I call CreateVolume
  And a basic block volume request "integration3" "8"
  And I call CreateVolume
  When I call CreateVolumeGroupSnapshot
  And remove a volume from VolumeGroupSnapshotRequest 
  When I call CreateVolumeGroupSnapshot
  And I call DeleteVGS
  And when I call DeleteAllVolumes
  Then the error message should contain "contains more snapshots"

Scenario: Call ControllerGetVolume with Good VolumeID
  Given a VxFlexOS service
  And a capability with voltype "mount" access "single-writer" fstype "ext4"
  And a volume request "integration19" "8"
  When I call CreateVolume
  And there are no errors
  And when I call PublishVolume "SDC_GUID"
  And there are no errors
  And when I call NodePublishVolume "SDC_GUID"
  And there are no errors
  And I call ControllerGetVolume
  And the volumecondition is "healthy"
  And when I call NodeUnpublishVolume "SDC_GUID"
  And there are no errors
  And when I call UnpublishVolume "SDC_GUID"
  And there are no errors
  And when I call DeleteVolume
  Then there are no errors

Scenario: Call ControllerGetVolume with No VolumeID
  Given a VxFlexOS service
  And a capability with voltype "mount" access "single-writer" fstype "ext4"
  And a volume request "integration19" "8"
  When I call CreateVolume
  And there are no errors
  And when I call PublishVolume "SDC_GUID"
  And there are no errors
  And when I call NodePublishVolume "SDC_GUID"
  And when I call NodeUnpublishVolume "SDC_GUID"
  And when I call UnpublishVolume "SDC_GUID"
  And there are no errors
  And when I call DeleteVolume
  Then there are no errors
  And I call ControllerGetVolume
  And the volumecondition is "unhealthy"
  Then there are no errors

Scenario: Call NodeGetVolumeStats on volume 
  Given a VxFlexOS service
  And a capability with voltype "mount" access "single-writer" fstype "ext4"
  And a volume request "integration" "8"
  When I call CreateVolume
  And there are no errors
  And when I call PublishVolume "SDC_GUID"
  And there are no errors
  And when I call NodePublishVolume "SDC_GUID"
  And I call NodeGetVolumeStats
  And the VolumeCondition is "ok"
  And when I call NodeUnpublishVolume "SDC_GUID"
  And when I call UnpublishVolume "SDC_GUID"
  And there are no errors
  And when I call DeleteVolume
  Then there are no errors

Scenario: Call NodeGetVolumeStats on unmounted volume
  Given a VxFlexOS service
  And a capability with voltype "mount" access "single-writer" fstype "ext4"
  And a volume request "integration" "8"
  When I call CreateVolume
  And there are no errors
  And when I call PublishVolume "SDC_GUID"
  And there are no errors
  And when I call NodePublishVolume "SDC_GUID"
  And when I call NodeUnpublishVolume "SDC_GUID"
  And I call NodeGetVolumeStats
  And the VolumeCondition is "abnormal"
  And when I call UnpublishVolume "SDC_GUID"
  And there are no errors
  And when I call DeleteVolume
  Then there are no errors

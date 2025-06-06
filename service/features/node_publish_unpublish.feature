Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test list service methods
  So that they are known to work

  Scenario Outline: Node publish various use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    When I call Probe
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | voltype | access                      | fstype | errormsg                                                          |
      | "mount" | "single-writer"             | "xfs"  | "none"                                                            |
      | "mount" | "single-writer"             | "ext4" | "none"                                                            |
      | "mount" | "multiple-writer"           | "ext4" | "Mount volumes do not support AccessMode MULTI_NODE_MULTI_WRITER" |
      | "block" | "single-writer"             | "none" | "none"                                                            |
      | "block" | "single-reader"             | "none" | "none"                                                            |
      | "block" | "multiple-writer"           | "none" | "none"                                                            |
      | "mount" | "single-node-single-writer" | "none" | "none"                                                            |
      | "mount" | "single-node-multi-writer"  | "none" | "none"                                                            |

  Scenario Outline: Node publish block volumes various induced error use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Publish Volume Request
    And I induce error <error>
    When I call Probe
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | error                                    | errormsg                                                    |
      | "NodePublishBlockTargetNotFile"          | "existing path is a directory"                              |
      | "GOFSMockBindMountError"                 | "none"                                                      |
      | "GOFSMockMountError"                     | "error bind mounting to target path"                        |
      | "GOFSMockGetMountsError"                 | "Could not getDevMounts"                                    |
      | "NoSymlinkForNodePublish"                | "not published to node"                                     |
      # may be different for Windows vs. Linux
      | "NoBlockDevForNodePublish"               | "error getting block device for volume@@no such file or directory" |
      | "TargetNotCreatedForNodePublish"         | "none"                                                      |
      # may be different for Windows vs. Linux
      | "PrivateDirectoryNotExistForNodePublish" | "cannot find the path specified@@no such file or directory" |
      | "BlockMkfilePrivateDirectoryNodePublish" | "existing path is not a directory"                          |
      | "NodePublishNoTargetPath"                | "target path required"                                      |
      | "NodePublishNoVolumeCapability"          | "volume capability required"                                |
      | "NodePublishNoAccessMode"                | "Volume Access Mode is required"                            |
      | "NodePublishNoAccessType"                | "Volume Access Type is required"                            |
      | "NodePublishBadTargetPath"               | "cannot find the path specified@@no such file or directory" |
      | "none"                                   | "none"                                                      |

  Scenario Outline: Node publish mount volumes various induced error use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    And I induce error <error>
    When I call Probe
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | error                                    | errormsg                               |
      | "NodePublishPrivateTargetAlreadyMounted" | "Mount point already in use by device" |

  Scenario: a Basic NFS Node Publish unpublish Volume no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodeUnpublishVolume ""
    Then the error contains "none"


   Scenario: a Basic NFS Node Publish filesystem not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-writer" fstype "nfs"
    And I set bad FileSystem Id
    Then I call NodePublishVolume NFS ""
    Then the error contains "filesystem not found"


    Scenario: a Basic NFS Node Publish Volume GetFileSystemsById error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-writer" fstype "nfs"
    And I induce error "GetFileSystemsByIdError"
    Then I call NodePublishVolume NFS ""
    Then the error contains "filesystem not found"


   Scenario: a Basic NFS Node Publish unpublish Volume no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodeUnpublishVolume ""
    Then the error contains "none"


    Scenario: a Basic Idempotent NFS Node Publish Unpublish Volume no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodeUnpublishVolume ""
    Then the error contains "none"


    Scenario: a Basic NFS Node Publish Volume NAS server not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
     And I induce error "NasNotFoundError"
    Then I call NodePublishVolume NFS ""
    Then the error contains "could not find NAS server by id"


    Scenario: a Basic NFS Node Publish Volume File interface not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-single-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
     And I induce error "fileInterfaceNotFoundError"
    Then I call NodePublishVolume NFS ""
    Then the error contains "could not find the File interface using id"


    Scenario: a Basic NFS Node Publish Volume unknown access mode error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-reader"
    Then the error contains "access mode cannot be UNKNOWN"


    Scenario: a Basic NFS Node Publish Unpublish Volume no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "multiple-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodeUnpublishVolume ""
    Then the error contains "none"



    Scenario: a Basic NFS Node Publish Unpublish Volume filesystem not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "multiple-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    And I set bad FileSystem Id
    Then I call NodeUnpublishVolume ""
    Then the error contains "filesystem not found"


    Scenario: a Basic NFS Node Publish Unpublish Volume GetFileSystemsById error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "multiple-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    And I induce error "GetFileSystemsByIdError"
    Then I call NodeUnpublishVolume ""
    Then the error contains "filesystem not found"


    Scenario Outline: Node Publish Unpublish mount volumes various induced error use cases from examples NFS volumes
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "multiple-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    And I induce error <error>
    And I call NodeUnpublishVolume ""
    Then the error contains <errormsg>

    Examples:
      | error                                    | errormsg                                             |
      | "NodeUnpublishBadVolume"                 | "none"                                               |
      | "GOFSMockGetMountsError"                 | "could not reliably determine existing mount status" |
      | "NodeUnpublishNoTargetPath"              | "target path argument is required"                   |
      | "GOFSMockUnmountError"                   | "error unmounting target"                            |
      | "PrivateDirectoryNotExistForNodePublish" | "none"                                               |
      | "NoCsiVolIDError"                        | "volume ID is required"                              |
      | "none"                                   | "none"                                               |


    Scenario: a Basic NFS Node Publish Unpublish Volume no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-multi-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-multi-writer" fstype "nfs"
    Then I call NodePublishVolume NFS ""
    Then the error contains "none"
    Then I call NodeUnpublishVolume ""
    Then the error contains "none"

  Scenario Outline: Node publish mount volumes various induced error use cases from examples NFS volumes
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-multi-writer"
    Then a valid PublishVolumeResponse is returned
    And a capability with voltype "mount" access "single-node-multi-writer" fstype "nfs"
    And get Node Publish Volume Request NFS
    And I induce error <error>
    And I induce error <errorb>
    When I call Probe
    Then I call NodePublishVolume NFS ""
    Then the error contains <errormsg>

    Examples:
      | error                                    | errorb                   | errormsg                                                    |
      | "GOFSMockDevMountsError"                 | "none"                   | "none"                                                      |
      | "GOFSMockMountError"                     | "none"                   | "mount induced error"                                       |
      | "GOFSMockGetMountsError"                 | "none"                   | "could not reliably determine existing mount status"        |
      | "TargetNotCreatedForNodePublish"         | "none"                   | "none"                                                      |
      | "NodePublishNoTargetPath"                | "none"                   | "Target Path is required"                                   |
      | "NodePublishNoVolumeCapability"          | "none"                   | "Volume Capability is required"                             |
      | "NodePublishNoAccessMode"                | "none"                   | "Volume Access Mode is required"                            |
      | "NodePublishNoAccessType"                | "none"                   | "Invalid access type"                                       |
      | "NodePublishPrivateTargetAlreadyMounted" | "GOFSMockGetMountsError" | "could not reliably determine existing mount status"        |
      | "NodePublishBadTargetPath"               | "none"                   | "cannot find the path specified@@no such file or directory" |
      | "NoCsiVolIDError"                        | "none"                   | "volume ID is required"                                     |

  Scenario Outline: Node publish mount volumes various induced error use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    And I induce error <error>
    And I induce error <errorb>
    When I call Probe
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | error                                    | errorb                   | errormsg                                                    |
      | "GOFSMockDevMountsError"                 | "none"                   | "none"                                                      |
      | "GOFSMockMountError"                     | "none"                   | "mount induced error"                                       |
      | "GOFSMockGetMountsError"                 | "none"                   | "could not reliably determine existing mount status"        |
      | "NoSymlinkForNodePublish"                | "none"                   | "not published to node"                                     |
      # may be different for Windows vs. Linux
      | "NoBlockDevForNodePublish"               | "none"                   | "error getting block device for volume@@no such file or directory" |
      | "TargetNotCreatedForNodePublish"         | "none"                   | "none"                                                      |
      # may be different for Windows vs. Linux
      | "PrivateDirectoryNotExistForNodePublish" | "none"                   | "cannot find the path specified@@no such file or directory" |
      | "BlockMkfilePrivateDirectoryNodePublish" | "none"                   | "existing path is not a directory"                          |
      | "NodePublishNoTargetPath"                | "none"                   | "target path required"                                      |
      | "NodePublishNoVolumeCapability"          | "none"                   | "volume capability required"                                |
      | "NodePublishNoAccessMode"                | "none"                   | "Volume Access Mode is required"                            |
      | "NodePublishNoAccessType"                | "none"                   | "Volume Access Type is required"                            |
      | "NodePublishFileTargetNotDir"            | "none"                   | "existing path is not a directory"                          |
      | "NodePublishPrivateTargetAlreadyCreated" | "none"                   | "not published to node"                                     |
      | "NodePublishPrivateTargetAlreadyMounted" | "none"                   | "Mount point already in use by device"                      |
      | "NodePublishPrivateTargetAlreadyMounted" | "GOFSMockGetMountsError" | "could not reliably determine existing mount status"        |
      | "NodePublishBadTargetPath"               | "none"                   | "cannot find the path specified@@no such file or directory" |
      | "NoCsiVolIDError"                        | "none"                   | "volume ID is required"                                     |

  Scenario: Induce legacy volume check failure
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Publish Volume Request
    And I induce error "RequireProbeFailError"
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains "is shorter than 3 chars, returning error"

  Scenario: Induce Node publish block volumes no system ID failure
    Given setup Get SystemID to fail
    And a VxFlexOS service
    And a controller published volume
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Publish Volume Request
    And I induce error "NoSysIDError"
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains "systemID is not found in the request and there is no default system"

  Scenario Outline: Node publish various use cases from examples when volume already published
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I induce error "NodePublishPathAltDataDir"
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | voltype | access            | fstype | errormsg                                                          |
      | "block" | "single-writer"   | "none" | "Access mode conflicts with existing mounts"                      |
      | "block" | "multiple-writer" | "none" | "none"                                                            |
      | "mount" | "single-writer"   | "xfs"  | "Access mode conflicts with existing mounts"                      |
      | "mount" | "single-writer"   | "ext4" | "Access mode conflicts with existing mounts"                      |
      | "mount" | "multiple-writer" | "ext4" | "Mount volumes do not support AccessMode MULTI_NODE_MULTI_WRITER" |
      | "block" | "multiple-reader" | "none" | "none"                                                            |

  Scenario Outline: Node publish various use cases from examples when read-only mount volume already published
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    And get Node Publish Volume Request
    And I mark request read only
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | voltype | access            | fstype | errormsg                                            |
      | "block" | "multiple-reader" | "none" | "none"                                              |
      | "mount" | "single-reader"   | "none" | "none"                                              |
      | "mount" | "single-reader"   | "xfs"  | "none"                                              |
      | "mount" | "multiple-reader" | "ext4" | "none"                                              |
      | "mount" | "multiple-writer" | "ext4" | "do not support AccessMode MULTI_NODE_MULTI_WRITER" |

  Scenario: Node publish but access modes conflicts
   Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And get Node Publish Volume Request
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I mark request read only
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains "Access mode conflicts with existing mounts"


  Scenario Outline: Node publish various use cases from examples when read-only mount volume already published and I change the target path
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    And get Node Publish Volume Request
    And I mark request read only
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    #And I change the target path
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | voltype | access            | fstype | errormsg                                            |
      | "mount" | "single-reader"   | "none" | "none"                                              |
      | "mount" | "single-reader"   | "xfs"  | "none"                                              |
      | "block" | "multiple-reader" | "none" | "none"                                              |
      | "mount" | "multiple-reader" | "ext4" | "none"                                              |
      #| "mount" | "single-writer"   | "ext4" | "Access mode conflicts with existing mounts"        |
      | "mount" | "multiple-writer" | "ext4" | "do not support AccessMode MULTI_NODE_MULTI_WRITER" |

  Scenario:  Node publish when read-only mount volume already published and I change the target path, access mode conflicts
   Given a VxFlexOS service
   And a controller published volume
   And a capability with voltype "mount" access "single-writer" fstype "ext4"
   And get Node Publish Volume Request
   When I call Probe
   And I call NodePublishVolume "SDC_GUID"
   #And I change the target path
   And I mark request read only
   And I call NodePublishVolume "SDC_GUID"
   Then the error contains "Access mode conflicts with existing mounts"

  Scenario: Node publish volume with volume context
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-reader" fstype "none"
    And get Node Publish Volume Request
    And I give request volume context
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains "none"

  Scenario Outline: Node Unpublish various use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call NodeUnpublishVolume "SDC_GUID"
    And there are no remaining mounts
    Then the error contains <errormsg>

    Examples:
      | voltype | access            | fstype | errormsg                                                          |
      | "block" | "single-writer"   | "none" | "none"                                                            |
      | "block" | "multiple-writer" | "none" | "none"                                                            |
      | "mount" | "single-writer"   | "xfs"  | "none"                                                            |
      | "mount" | "multi-pod-rw"    | "none" | "Mount volumes do not support AccessMode MULTI_NODE_MULTI_WRITER" |
      | "block" | "multi-pod-rw"    | "none" | "none"                                                            |

  Scenario Outline: Node Unpublish doesn't unmount private mount if another pod is using the mount
    Given a VxFlexOS service
    And a controller published volume with the private target equalling the mount path
    And a capability with voltype <voltype> access <access> fstype <fstype>
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I create mount <mount>
    And I call NodeUnpublishVolume "SDC_GUID"
    And there are remaining mounts
    Then the error contains <errormsg>

    Examples:
      | voltype | access                      | fstype | | mount | errormsg |
      | "block" | "multi-pod-rw"              | "none" | |  "test/070aa5c2-3a1a-4f55-836a-a7d81ab9cce5/d0f055a700000000"    | "none" |
      | "mount" | "single-node-multi-writer"  | "xfs" |  |  "test/070aa5c2-3a1a-4f55-836a-a7d81ab9cce5/d0f055a700000000"     | "none" |

  Scenario Outline: Node Unpublish mount volumes various induced error use cases from examples
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I induce error <error>
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | error                                    | errormsg                                             |
      | "NodeUnpublishBadVolume"                 | "none"                                               |
      | "GOFSMockGetMountsError"                 | "could not reliably determine existing mount status" |
      | "NodeUnpublishNoTargetPath"              | "target path argument is required"                   |
      | "GOFSMockUnmountError"                   | "Error unmounting target"                            |
      | "PrivateDirectoryNotExistForNodePublish" | "none"                                               |
      | "NoCsiVolIDError"                        | "volume ID is required"                              |
      | "none"                                   | "none"                                               |

  Scenario: Induce Node Unpublish mount volumes ephemeral ID failure
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I induce error <error>
    And I create false ephemeral ID
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
      | error                  | errormsg                                   |
      | "IncorrectEphemeralID" | "none"                                     |
      | "EmptyEphemeralID"     | "is shorter than 3 chars, returning error" |

  Scenario: Induce Node publish block volumes no system ID failure
    Given setup Get SystemID to fail
    And a VxFlexOS service
    And a controller published volume
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Publish Volume Request
    And I call NodePublishVolume "SDC_GUID"
    And I induce error "NoSysIDError"
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains "systemID is not found in the request and there is no default system"

  Scenario: Get device given invalid path
    Given a VxFlexOS service
    When I call GetDevice "INVALIDPATH"
    Then the error contains "invalid path error"

  Scenario Outline: Call getMappedVols with correct and incorrect inputs
    Given two identical volumes on two different systems
    When I call getMappedVols with volID <volID> and sysID <sysID>
    Then the error contains <errormsg>

    Examples:
      | volID              | sysID              | errormsg                                                                     |
      | "c0f055aa00000000" | "34dbbf5617523654" | "none"                                                                       |
      | "c0f055aa00000000" | "14dbbf5617523654" | "volume: c0f055aa00000000 on system: 14dbbf5617523654 not published to node" |

  Scenario: Check that getMappedVols returns correct volume from correct system
    Given two identical volumes on two different systems
    When I call getMappedVols with volID "c0f055aa00000000" and sysID "34dbbf5617523654"
    Then the volume "c0f055aa00000000" is from the correct system "34dbbf5617523654"

  Scenario: Call CleanupPrivateTarget to verify that when target mounts exist, private target is not deleted
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-reader" fstype "none"
    And get Node Publish Volume Request
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call CleanupPrivateTarget
    Then the error contains "Cannot delete private mount as target mount exist"

  Scenario: Call removeWithRetry negative test
    Given a VxFlexOS service
    And I call removeWithRetry
    Then the error contains "failed to remove directory:"

  Scenario: Call I call unmountPrivMount negative test
    Given a VxFlexOS service
    And I call unmountPrivMount
    Then the error contains "error in unmountPrivMount"

  Scenario: Call I call getPathMounts negative test
    Given a VxFlexOS service
    And I call getPathMounts
    Then the error contains "error in GetPathMounts"

  Scenario: Call handlePrivFSMount  negative test
    Given a VxFlexOS service
    And I call handlePrivFSMount
    Then the error contains "error in handlePrivFSMount"

  Scenario: Call evalSymlinks negative test
    Given a VxFlexOS service
    And I call evalSymlinks
    Then the error contains "error in evalSymlinks"

  Scenario: Call evalSymlinks negative test
    Given a VxFlexOS service
    And I call CleanupPrivateTarget for errors
    Then the error contains "error in CleanupPrivateTarget"

  Scenario: mount publishVolume negative test
    Given a VxFlexOS service
    And I call mount publishVolume
    Then the error contains "error in publishVolume"

  Scenario: Call unpublishVolume negative test
    Given a VxFlexOS service
    And I call mount unpublishVolume
    Then the error contains "error in unpublishVolume"

  Scenario: Call mount validateVolCapabilities negative test
    Given a VxFlexOS service
    And I call mountValidateBlockVolCapabilities
    Then the error contains "Unknown Access Mode"

  Scenario: Check if the CleanupPrivateTarget target deletes private target when there are no target mounts.
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-reader" fstype "none"
    And get Node Publish Volume Request
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call UnmountAndDeleteTarget
    And I call CleanupPrivateTarget
    Then the error contains "none"
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains "none"

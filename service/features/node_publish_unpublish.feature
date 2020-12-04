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
      | voltype | access            | fstype | errormsg                                                          |
      | "mount" | "single-writer"   | "xfs"  | "none"                                                            |
      | "mount" | "single-writer"   | "ext4" | "none"                                                            |
      | "mount" | "multiple-writer" | "ext4" | "Mount volumes do not support AccessMode MULTI_NODE_MULTI_WRITER" |
      | "block" | "single-writer"   | "none" | "none"                                                            |
      | "block" | "multiple-writer" | "none" | "none"                                                            |

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
      | "NoBlockDevForNodePublish"               | "is not a block device@@not published to node"              |
      | "TargetNotCreatedForNodePublish"         | "none"                                                      |
      # may be different for Windows vs. Linux
      | "PrivateDirectoryNotExistForNodePublish" | "cannot find the path specified@@no such file or directory" |
      | "BlockMkfilePrivateDirectoryNodePublish" | "existing path is not a directory"                          |
      | "NodePublishNoTargetPath"                | "target path required"                                      |
      | "NodePublishNoVolumeCapability"          | "volume capability required"                                |
      | "NodePublishNoAccessMode"                | "Volume Access Mode is required"                            |
      | "NodePublishNoAccessType"                | "Volume Access Type is required"                            |
      | "NodePublishBadTargetPath"               | "cannot find the path specified@@no such file or directory" |

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
      | "NoBlockDevForNodePublish"               | "none"                   | "is not a block device@@not published to node"              |
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
      | "NodePublishPrivateTargetAlreadyMounted" | "none"                   | "Mount point already in use by device@@none"                |
      | "NodePublishPrivateTargetAlreadyMounted" | "GOFSMockGetMountsError" | "could not reliably determine existing mount status"        |
      | "NodePublishBadTargetPath"               | "none"                   | "cannot find the path specified@@no such file or directory" |

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
      | "block" | "multiple-reader" | "none" | "read only not supported for Block Volume"          |
      | "mount" | "single-reader"   | "none" | "none"                                              |
      | "mount" | "single-reader"   | "xfs"  | "none"                                              |
      | "mount" | "multiple-reader" | "ext4" | "none"                                              |
      | "mount" | "single-writer"   | "ext4" | "Access mode conflicts with existing mounts"        |
      | "mount" | "multiple-writer" | "ext4" | "do not support AccessMode MULTI_NODE_MULTI_WRITER" |

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
      | "block" | "multiple-reader" | "none" | "read only not supported for Block Volume"          |
      | "mount" | "multiple-reader" | "ext4" | "none"                                              |
      | "mount" | "single-writer"   | "ext4" | "Access mode conflicts with existing mounts"        |
      | "mount" | "multiple-writer" | "ext4" | "do not support AccessMode MULTI_NODE_MULTI_WRITER" |

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
      | voltype | access            | fstype | errormsg |
      | "block" | "single-writer"   | "none" | "none"   |
      | "block" | "multiple-writer" | "none" | "none"   |
      | "mount" | "single-writer"   | "xfs"  | "none"   |

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

  Scenario: Get device given invalid path
    Given a VxFlexOS service
    When I call GetDevice "INVALIDPATH"
    Then the error contains "invalid path error"

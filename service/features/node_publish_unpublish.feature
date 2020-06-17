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
    | voltype      | access                         | fstype     | errormsg                                     |
    | "mount"      | "single-writer"                | "xfs"      | "none"                                       |
    | "mount"      | "single-writer"                | "ext4"     | "none"                                       |
    | "mount"      | "multiple-writer"              | "ext4"     | "Invalid access mode"                        |
    | "block"      | "single-writer"                | "none"     | "none"                                       |
    | "block"      | "multiple-writer"              | "none"     | "none"                                       |
       
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
    | error                                   | errormsg                                                   |
    | "GOFSMockBindMountError"                | "none"                                                     |
    | "GOFSMockMountError"                    | "failure bind-mounting block device to private mount"      |
    | "GOFSMockGetMountsError"                | "could not reliably determine existing mount status"       |
    | "NoSymlinkForNodePublish"               | "not published to node"                                    |
    # may be different for Windows vs. Linux
    | "NoBlockDevForNodePublish"              | "is not a block device@@not published to node"             |
    | "TargetNotCreatedForNodePublish"        | "none"                                                     |
    # may be different for Windows vs. Linux
    | "PrivateDirectoryNotExistForNodePublish"| "cannot find the path specified@@no such file or directory"|
    | "BlockMkfilePrivateDirectoryNodePublish"| "existing path is not a directory"                         |
    | "NodePublishNoTargetPath"               | "target path required"                                     |
    | "NodePublishNoVolumeCapability"         | "volume capability required"                               |
    | "NodePublishNoAccessMode"               | "volume access mode required"                              |
    | "NodePublishNoAccessType"               | "volume access type required"                              |
    | "NodePublishBlockTargetNotFile"         | "wrong type (file vs dir)"                                 |

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
    | error                                   | errormsg                                                    |
    | "GOFSMockDevMountsError"                | "none"                                                      |
    | "GOFSMockMountError"                    | "mount induced error"                                       |
    | "GOFSMockGetMountsError"                | "could not reliably determine existing mount status"        |
    | "NoSymlinkForNodePublish"               | "not published to node"                                     |
    # may be different for Windows vs. Linux
    | "NoBlockDevForNodePublish"              | "is not a block device@@not published to node"              |
    | "TargetNotCreatedForNodePublish"        | "none"                                                      |
    # may be different for Windows vs. Linux
    | "PrivateDirectoryNotExistForNodePublish"| "cannot find the path specified@@no such file or directory" |
    | "BlockMkfilePrivateDirectoryNodePublish"| "existing path is not a directory"                          |
    | "NodePublishNoTargetPath"               | "target path required"                                      |
    | "NodePublishNoVolumeCapability"         | "volume capability required"                                |
    | "NodePublishNoAccessMode"               | "volume access mode required"                               |
    | "NodePublishNoAccessType"               | "volume access type required"                               |
    | "NodePublishFileTargetNotDir"           | "wrong type (file vs dir)"                                  |


  Scenario Outline: Node publish various use cases from examples when volume already published
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype <voltype> access <access> fstype <fstype>
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call NodePublishVolume "SDC_GUID"
    Then the error contains <errormsg>

    Examples:
    | voltype      | access                         | fstype     | errormsg                                     |
    | "block"      | "single-writer"                | "none"     | "access mode conflicts with existing mounts" |
    | "mount"      | "single-writer"                | "xfs"      | "access mode conflicts with existing mounts" |
    | "mount"      | "single-writer"                | "ext4"     | "access mode conflicts with existing mounts" |
    | "mount"      | "multiple-writer"              | "ext4"     | "Invalid access mode"                        |
# The following line seems like the wrong behavior; shouldn't this be allowed?
    | "block"      | "multiple-reader"              | "none"     | "access mode conflicts with existing mounts" |

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
    | voltype      | access                         | fstype     | errormsg                                     |
    | "block"      | "multiple-reader"              | "none"     | "read only not supported for Block Volume"   |
    | "mount"      | "single-reader"                | "none"     | "none"                                       |
    | "mount"      | "single-reader"                | "xfs"      | "none"                                       |
    | "mount"      | "multiple-reader"              | "ext4"     | "none"                                       |
    | "mount"      | "single-writer"                | "ext4"     | "access mode conflicts with existing mounts" |
    | "mount"      | "multiple-writer"              | "ext4"     | "Invalid access mode"                        |

  Scenario Outline: Node publish various use cases from examples when read-only mount volume already published
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
    | voltype      | access                         | fstype     | errormsg                                     |
    | "mount"      | "single-reader"                | "none"     | "none"                                       |
    | "mount"      | "single-reader"                | "xfs"      | "none"                                       |
    | "block"      | "multiple-reader"              | "none"     | "read only not supported for Block Volume"   |
    | "mount"      | "multiple-reader"              | "ext4"     | "none"                                       |
    | "mount"      | "single-writer"                | "ext4"     | "access mode conflicts with existing mounts" |
    | "mount"      | "multiple-writer"              | "ext4"     | "Invalid access mode"                        |


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
    | voltype      | access                         | fstype     | errormsg                                     |
    | "block"      | "single-writer"                | "none"     | "none"                                       |
    | "block"      | "multiple-writer"              | "none"     | "none"                                       |
    | "mount"      | "single-writer"                | "xfs"      | "none"                                       |

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
    | error                                   | errormsg                                                    |
    | "NodeUnpublishBadVolume"                | "none"                                                      |
    | "GOFSMockGetMountsError"                | "could not reliably determine existing mount status"        |
    | "NodeUnpublishNoTargetPath"             | "target path required"                                      |
    | "GOFSMockUnmountError"                  | "Error unmounting target"                                   |
    | "PrivateDirectoryNotExistForNodePublish"| "none"                                                      |

  Scenario: Get device given invalid path
    Given a VxFlexOS service
    When I call GetDevice "INVALIDPATH"
    Then the error contains "invalid path error"

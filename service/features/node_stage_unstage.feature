Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test list service methods
  So that they are known to work

  Scenario Outline: Node stage volume input validation
    Given a VxFlexOS service
    And I set protocol to <protocol>
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Stage Volume Request
    And I induce error <error>
    When I call Probe
    When I call NodeStageVolume
    Then the error contains <errormsg>
    Examples:
      | protocol  | error                      | errormsg                                                |
      | "SDC"     | "none"                     | "none"                                                  |
      | "NVMeTCP" | "NodeStageNoVolumeID"      | "volume ID is required"                                 |
      | "NVMeTCP" | "NodeStageInValidVolumeID" | "failed to build NGUID: volumeID must be 16 characters" |
      | "NVMeTCP" | "NodeStageNoCapability"    | "volume capability is required"                         |
      | "NVMeTCP" | "NodeStageNoAccessMode"    | "Volume Access Mode is required"                        |
      | "NVMeTCP" | "NodeStageNoStagingPath"   | "staging target path is required"                       |

  Scenario Outline: Node stage block volume various induced error use cases from examples
    Given a VxFlexOS service
    And I set protocol to "NVMeTCP"
    And a capability with voltype "block" access "single-writer" fstype "none"
    And I induce error <error>
    When I call Probe
    When I call NodeStageVolume
    Then the error contains <errormsg>
    Examples:
      | error                                    | errormsg                                                                        |
      | "none"                                   | "none"                                                                          |
      | "GobrickConnectError"                    | "unable to find device: induced ConnectVolumeError"                             |
      | "GOFSMockGetDiskFormatError"             | "failed to probe staging state: disk format probe: getDiskFormat induced error" |
      | "GOFSMockGetDiskFormatType_mpath_member" | "none"                                                                          |
      | "GOFSMockGetMounts_deleted"              | "none"                                                                          |
      | "GOFSMockGetMountsError"                 | "failed to probe staging state: get mounts: getMounts induced error"            |

  Scenario Outline: Node stage mount volume various induced error use cases from examples
    Given a VxFlexOS service
    And I set protocol to "NVMeTCP"
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And I induce error <error>
    When I call Probe
    When I call NodeStageVolume
    Then the error contains <errormsg>
    Examples:
      | error                                    | errormsg                                                                        |
      | "none"                                   | "none"                                                                          |
      | "GobrickConnectError"                    | "unable to find device: induced ConnectVolumeError"                             |
      | "GOFSMockGetDiskFormatError"             | "failed to probe staging state: disk format probe: getDiskFormat induced error" |
      | "GOFSMockGetDiskFormatType_mpath_member" | "none"                                                                          |
      | "GOFSMockGetMounts_deleted"              | "none"                                                                          |
      | "GOFSMockGetMountsError"                 | "failed to probe staging state: get mounts: getMounts induced error"            |

  Scenario Outline: Node Unstage volume various induced error use cases from examples
    Given a VxFlexOS service
    And I set protocol to "NVMeTCP"
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And I induce error <error>
    When I call Probe
    And I call NodeUnstageVolume with "none"
    Then the error contains <errormsg>
    Examples:
      | error                                     | errormsg                                 |
      | "none"                                    | "none"                                   |
      | "GobrickDisconnectError"                  | "Failed to disconnect NVME device"       |
      | "GOFSMockGetMountsError"                  | "none"                                   |
      | "GOFSMockGetMounts_deleted"               | "none"                                   |
      | "GOFSMockUnmountError"                    | "none"                                   |
      | "GOFSMockGetMounts_unknowndevice"         | "none"                                   |
      
  Scenario Outline: Idempotent Node stage volume
    Given a VxFlexOS service
    And I set protocol to "NVMeTCP"
    And a capability with voltype <volType> access "single-writer" fstype "none"
    When I call Probe
    When I call NodeStageVolume
    Then the error contains "none"
    When I call NodeStageVolume
    Then the error contains "none"
    Examples:
      | volType |
      | "block" |
      | "mount" |

  Scenario Outline: Idempotent Node Unstage volume
    Given a VxFlexOS service
    And I set protocol to "NVMeTCP"
    And a capability with voltype <volType> access "single-writer" fstype "none"
    When I call Probe
    When I call NodeUnstageVolume with "none"
    Then the error contains "none"
    When I call NodeUnstageVolume with "none"
    Then the error contains "none"
    Examples:
      | volType |
      | "block" |
      | "mount" |

  Scenario Outline: Call NodeUnstageVolume to test podmon functionality
    Given a VxFlexOS service
    And I call Probe
    When I call NodeUnstageVolume with <error>
    Then the error contains <errormsg>
    Examples:
      | error             | errormsg                               |
      | "none"            | "none"                                 |
      | "NoRequestID"     | "none"                                 |
      | "NoVolumeID"      | "Volume ID is required"                |
      | "NoStagingTarget" | "StagingTargetPath is required"        |
      | "EphemeralVolume" | "none"                                 |
      | "UnmountError"    | "Unable to remove staging target path" |
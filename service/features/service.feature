Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test service methods
  So that they are known to work

  Scenario: Call checkVolumesMap when volumes cannot be listed
    Given a VxFlexOS service
    And a valid volume
    And I call Probe
    And I induce error "VolumeInstancesError"
    And I call checkVolumesMap "123"
    Then the error contains "failed to list vols for array"

  Scenario Outline: Test calls to updateVolumesMap with system already present
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call UpdateVolumePrefixToSystemsMap <systemName>
    Then the error contains <errorMsg>

    Examples:
      | systemName                     | errorMsg |
      | "14dbbf5617523654"             | "none"   |
      | "15dbbf5617523655-system-name" | "none"   |

  Scenario: Identity GetPluginInfo good call
    Given a VxFlexOS service
    When I call GetPluginInfo
    When I call BeforeServe
    Then a valid GetPlugInfoResponse is returned

  Scenario Outline: Dynamic log config change
    Given a VxFlexOS service
    When I call DynamicLogChange <file>
    Then a valid DynamicLogChange occurs <file> <level>
    Examples:
      | file                  | level   |
      | "logConfig2.yaml"     | "trace" |
      | "logConfigWrong.yaml" | "debug" |

  Scenario: Dynamic array config change
    Given a VxFlexOS service
    When I call DynamicArrayChange
    Then a valid DynamicArrayChange occurs

  Scenario Outline: multi array getSystemIDFromParameters good and with errors
    Given setup Get SystemID to fail
    Given a VxFlexOS service
    And I call GetSystemIDFromParameters with bad params <option>
    Then the error contains <errormsg>
    Examples:
      | option          | errormsg                 |
      | "good"          | "none"                   |
      | "NilParams"     | "params map is nil"      |
      | "NoSystemIDkey" | "No system ID is found " |

  Scenario Outline: multi array getVolumeIDFromCsiVolumeID good and with errors
    Given a VxFlexOS service
    And I call getVolumeIDFromCsiVolumeID <csiVolID>
    Then the error contains <errormsg>
    Examples:
      | csiVolID        | errormsg        |
      | "good"          | "good"          |
      | "NilParams"     | "NilParams"     |
      | "NoSystemIDkey" | "NoSystemIDkey" |

  Scenario Outline: multi array getVolumeIDFromCsiVolumeID good and with errors
    Given a VxFlexOS service
    And I call getVolumeIDFromCsiVolumeID <csiVolID>
    Then the error contains <errormsg>
    Examples:
      | csiVolID | errormsg |
      | "a"      | ""       |
      | "a-b"    | "b"      |
      | "a:b"    | "a:b"    |
      | "a:b"    | "a:b"    |
      | ""       | ""       |

  Scenario Outline: multi array getSystemIDFromCsiVolumeID good and with errors
    Given a VxFlexOS service
    And I call getSystemIDFromCsiVolumeID <csiVolID>
    Then the error contains <errormsg>
    Examples:
      | csiVolID | errormsg |
      | "a"      | ""       |
      | "a-b"    | "a"      |
      | "a:b"    | ""       |

  Scenario: Identity GetPluginCapabilitiles good call
    Given a VxFlexOS service
    When I call GetPluginCapabilities
    Then a valid GetPluginCapabilitiesResponse is returned

  Scenario: Identity Probe good call
    Given a VxFlexOS service
    When I call Probe
    Then a valid ProbeResponse is returned


  Scenario: Identity Probe call no controller connection
    Given a VxFlexOS service
    And the Controller has no connection
    When I invalidate the Probe cache
    And I call Probe
    Then the error contains "unable to login to VxFlexOS Gateway"

  Scenario Outline: Probe Call with various errors
    Given a VxFlexOS service
    And I induce error <error>
    When I invalidate the Probe cache
    And I call Probe
    Then the error contains <msg>

    Examples:
      | error               | msg                                            |
      | "NoEndpointError"   | "missing VxFlexOS Gateway endpoint"            |
      | "NoUserError"       | "missing VxFlexOS MDM user"                    |
      | "NoPasswordError"   | "missing VxFlexOS MDM password"                |
      | "NoSysNameError"    | "missing VxFlexOS system name"                 |
      | "WrongSysNameError" | "unable to find matching VxFlexOS system name" |


  # This injected error fails on Windows with no SDC but passes on Linux with SDC
  Scenario: Identity Probe call node probe Lsmod error
    Given a VxFlexOS service
    And there is a Node Probe Lsmod error
    When I invalidate the Probe cache
    And I call Node Probe
    Then the possible error contains "scini kernel module not loaded"

  # This injected error fails on Windows with no SDC but passes on Linux with SDC
  Scenario: Identity Probe call node probe SdcGUID error
    Given a VxFlexOS service
    And there is a Node Probe SdcGUID error
    When I call Node Probe
    Then the possible error contains "unable to get SDC GUID"

  Scenario: Identity Probe call node probe drvCfg error
    Given a VxFlexOS service
    And there is a Node Probe drvCfg error
    When I call Node Probe
    Then the possible error contains "unable to get System Name via config or drv_cfg binary"

  # This injected error fails on Windows with no SDC but passes on Linux with SDC
  Scenario: Identity Probe call node probe RenameSDC error
    Given a VxFlexOS service
    And there is a Node Probe RenameSDC error
    When I call Node Probe
    Then the possible error contains "Failed to rename SDC"

  Scenario Outline: Create volume good scenario
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume <name>
    Then a valid CreateVolumeResponse is returned

    Examples:
      | name                                                |
      | "volume1"                                           |
      | "thisnameiswaytoolongtopossiblybeunder31characters" |


  Scenario: Create volume with admin error
    Given a VxFlexOS service
    When I call Probe
    And I induce error "NoAdminError"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create Volume with invalid probe cache, no endpoint, and no admin
    Given a VxFlexOS service
    When I induce error "NoAdminError"
    And I induce error "NoEndpointError"
    And I invalidate the Probe cache
    And I call CreateVolume "volume1"
    Then the error contains "No system ID is found in parameters or as default"

  Scenario: Idempotent create volume with duplicate volume name
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume2"
    And I call CreateVolume "volume2"
    Then a valid CreateVolumeResponse is returned

  Scenario: Idempotent create volume with different sizes
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolumeSize "volume3" "8"
    And I call CreateVolumeSize "volume3" "16"
    Then the error contains "different size than requested"

  Scenario: Idempotent create volume with different sizes and induced error in handleQueryVolumeIDByKey
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolumeSize "volume3" "8"
    And I induce error "FindVolumeIDError"
    And I call CreateVolumeSize "volume3" "16"
    Then the error contains "induced error"

  Scenario: Idempotent create volume with different sizes and induced error in handleInstances
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolumeSize "volume3" "8"
    And I induce error "GetVolByIDError"
    And I call CreateVolumeSize "volume3" "16"
    Then the error contains "induced error"

  Scenario: Idempotent create volume with different sizes and induced error in handleStoragePoolInstances
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolumeSize "volume3" "8"
    And I induce error "GetStoragePoolsError"
    And I call CreateVolumeSize "volume3" "16"
    Then the error contains "induced error"

  Scenario: Idempotent create volume with different storage pool
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume4"
    And I change the StoragePool "other_storage_pool"
    And I call CreateVolume "volume4"
    Then the error contains "different storage pool"

  Scenario: Idempotent create volume with bad storage pool
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume4"
    And I change the StoragePool "no_storage_pool"
    And I call CreateVolume "volume4"
    Then the error contains "Couldn't find storage pool"

  Scenario Outline: Create volume with Accessibility Requirements
    Given a VxFlexOS service
    When I call Probe
    And I specify AccessibilityRequirements with a SystemID of <sysID>
    And I call CreateVolume "accessibility"
    Then the error contains <errormsg>

    Examples:
      | sysID                      | errormsg                               |
      | "f.service.opt.SystemName" | "none"                                 |
      | ""                         | "is not accessible based on Preferred" |
      | "Unknown"                  | "is not accessible based on Preferred" |
      | "badSystem"                | "is not accessible based on Preferred" |

  Scenario Outline: Create volume with Accessibility Requirements
    Given a VxFlexOS service
    When I call Probe
    And I specify AccessibilityRequirements with a SystemID of <sysID>
    And I call CreateVolume "accessibility"
    Then a valid CreateVolumeResponse with topology is returned
    Examples:
      | sysID                      |
      | "f.service.opt.SystemName" |

  Scenario: Create volume with AccessMode_MULTINODE_WRITER
    Given a VxFlexOS service
    When I call Probe
    And I specify MULTINODE_WRITER
    And I call CreateVolume "multi-writer"
    Then a valid CreateVolumeResponse is returned

  Scenario: Attempt create volume with no name
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume ""
    Then the error contains "Name cannot be empty"

  Scenario: Create volume with bad capacity
    Given a VxFlexOS service
    When I call Probe
    And I specify a BadCapacity
    And I call CreateVolume "bad capacity"
    Then the error contains "bad capacity"

  Scenario: Create volume with no storage pool
    Given a VxFlexOS service
    When I call Probe
    And I specify NoStoragePool
    And I call CreateVolume "no storage pool"
    Then the error contains "storagepool is a required parameter"

  Scenario: Create mount volume good scenario
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "xfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create mount volume idempotent test
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "xfs"
    And I call CreateVolume "volume2"
    And I call CreateVolume "volume2"
    Then a valid CreateVolumeResponse is returned

  Scenario: Call NodeGetInfo and validate NodeId
    Given a VxFlexOS service
    When I call NodeGetInfo
    Then a valid NodeGetInfoResponse is returned

  Scenario: Call GetCapacity without specifying Storage Pool Name (this returns overall capacity)
    Given a VxFlexOS service
    When I call Probe
    And I call GetCapacity with storage pool ""

  Scenario: Call GetCapacity with valid Storage Pool Name
    Given a VxFlexOS service
    When I call Probe
    And I call GetCapacity with storage pool "viki_pool_HDD_20181031"
    Then a valid GetCapacityResponse is returned

  Scenario: Call GetCapacity with invalid Storage Pool name
    Given a VxFlexOS service
    When I call Probe
    And I call GetCapacity with storage pool "xxx"
    Then the error contains "unable to look up storage pool"

  Scenario: Call GetCapacity with induced error retrieving statistics
    Given a VxFlexOS service
    When I call Probe
    And I induce error "GetStatisticsError"
    And I call GetCapacity with storage pool "viki_pool_HDD_20181031"
    Then the error contains "unable to get system stats"
  
  Scenario: Call ControllerGetCapabilities with health monitor enabled
    Given a VxFlexOS service
    When I call ControllerGetCapabilities "true"
    Then a valid ControllerGetCapabilitiesResponse is returned
  
  Scenario: Call ControllerGetCapabilities with health monitor disabled
    Given a VxFlexOS service
    When I call ControllerGetCapabilities "false"
    Then a valid ControllerGetCapabilitiesResponse is returned

  Scenario Outline: Calls to validate volume capabilities
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume1"
    And a valid CreateVolumeResponse is returned
    And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
    Then the error contains <errormsg>

    Examples:
      | voltype | access                      | fstype | errormsg                                                         |
      | "block" | "single-writer"             | "none" | "none"                                                           |
      | "block" | "multi-reader"              | "none" | "none"                                                           |
      | "mount" | "multi-writer"              | "ext4" | "multi-node with writer(s) only supported for block access type" |
      | "mount" | "multi-node-single-writer"  | "ext4" | "multi-node with writer(s) only supported for block access type" |
      | "mount" | "single-node-single-writer" | "ext4" | "none"                                                           |
      | "mount" | "single-node-multi-writer"  | "ext4" | "none"                                                           |
      | "mount" | "unknown"                   | "ext4" | "access mode cannot be UNKNOWN"                                  |
      | "none " | "unknown"                   | "ext4" | "unknown access type is not Block or Mount"                      |

  Scenario Outline: Call validate volume capabilities with non-existent volume
    Given a VxFlexOS service
    When I call Probe
    And an invalid volume
    And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
    Then the error contains <errormsg>

    Examples:
      | voltype | access          | fstype | errormsg           |
      | "block" | "single-writer" | "none" | "volume not found" |

  Scenario Outline: Call with no probe volume to validate volume capabilities
    Given a VxFlexOS service
    When I invalidate the Probe cache
    And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
    Then the error contains <errormsg>

    Examples:
      | voltype | access          | fstype | errormsg                                                              |
      | "block" | "single-writer" | "none" | "systemID is not found in the request and there is no default system" |

  Scenario: Call with ValidateVolumeCapabilities with bad vol ID
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume1"
    And a valid CreateVolumeResponse is returned
    And I induce error "BadVolIDError"
    And I call ValidateVolumeCapabilities with voltype "block" access "single-writer" fstype "none"
    Then the error contains "volume not found"

  Scenario: Call NodeStageVolume, should get unimplemented
    Given a VxFlexOS service
    And I call Probe
    When I call NodeStageVolume
    Then the error contains "Unimplemented"

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
  
  Scenario: Call NodeGetCapabilities with health monitor feature enabled
    Given a VxFlexOS service
    And I call Probe
    When I call NodeGetCapabilities "true"
    Then a valid NodeGetCapabilitiesResponse is returned
  
  Scenario: Call NodeGetCapabilities with health monitor feature disabled
    Given a VxFlexOS service
    And I call Probe
    When I call NodeGetCapabilities "false"
    Then a valid NodeGetCapabilitiesResponse is returned

  Scenario: Snapshot a single block volume
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot "snap1"
    Then a valid CreateSnapshotResponse is returned

  Scenario: Idempotent test of snapshot a single block volume
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And I induce error <error>
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot "clone"
    And no error was received
    And I call CreateSnapshot "clone"
    Then the error contains <errormsg>

    Examples:
      | error          | errormsg                                                           |
      | "none"         | "none"                                                             |
      | "BadVolIDJSON" | "Failed to create snapshot -- GetVolume returned unexpected error" |

  Scenario: Request to create Snapshot with same name and different SourceVolumeID
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot "snap1"
    And no error was received
    And I call CreateVolume "A Different Volume"
    And a valid CreateVolumeResponse is returned
    And I induce error "WrongVolIDError"
    And I call CreateSnapshot "snap1"
    Then the error contains "Failed to create snapshot"

  Scenario: Snapshot a single block volume but receive error
    Given a VxFlexOS service
    When I call Probe
    And I induce error "CreateSnapshotError"
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot ""
    Then the error contains "snapshot name cannot be Nil"

  Scenario: Call snapshot create with invalid volume
    Given a VxFlexOS service
    And an invalid volume
    When I call Probe
    And I call CreateSnapshot "snap1"
    Then the error contains "volume not found"

  Scenario: Call snapshot create with no volume
    Given a VxFlexOS service
    And no volume
    When I call Probe
    And I call CreateSnapshot "snap1"
    Then the error contains "volume ID to be snapped is required"

  Scenario: Call snapshot with no probe
    Given a VxFlexOS service
    And an invalid volume
    When I invalidate the Probe cache
    And I call CreateSnapshot "snap1"
    Then the error contains "systemID is not found in the request and there is no default system"

  Scenario: Snapshot a block volume consistency group
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol2"
    And a valid CreateVolumeResponse is returned
    And I call CreateVolume "vol3"
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot "snap1"
    Then a valid CreateSnapshotResponse is returned

  Scenario: Delete a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I call DeleteSnapshot
    Then no error was received

  Scenario: Idempotent delete a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I call DeleteSnapshot
    Then no error was received
    And I call DeleteSnapshot
    Then no error was received

  Scenario: Delete a snapshot with bad Vol ID
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I induce error "BadVolIDError"
    And I call DeleteSnapshot
    Then no error was received

  Scenario: Delete a snapshot with no probe
    Given a VxFlexOS service
    And a valid snapshot
    When I invalidate the Probe cache
    And I call DeleteSnapshot
    Then the error contains "systemID is not found in the request and there is no default system"

  Scenario: Delete a snapshot with invalid volume
    Given a VxFlexOS service
    And an invalid volume
    When I call Probe
    And I call DeleteSnapshot
    Then the error contains "volume not found"

  Scenario: Delete a snapshot with no volume
    Given a VxFlexOS service
    And no volume
    When I call Probe
    And I call DeleteSnapshot
    Then the error contains "snapshot ID to be deleted is required"

  Scenario: Delete snapshot that is mapped to an SDC
    Given a VxFlexOS service
    And a valid snapshot
    And the volume is already mapped to an SDC
    When I call Probe
    And I call DeleteSnapshot
    Then the error contains "snapshot is in use by the following SDC"

  Scenario: Delete snapshot with induced remove volume error
    Given a VxFlexOS service
    And a valid snapshot
    And I induce error "RemoveVolumeError"
    When I call Probe
    And I call DeleteSnapshot
    Then the error contains "error removing snapshot"

  Scenario: Delete snapshot consistency group
    Given a VxFlexOS service
    And a valid snapshot consistency group
    When I call Probe
    And I call DeleteSnapshot
    Then no error was received
    And I call DeleteSnapshot
    Then no error was received

  Scenario: Delete snapshot consistency group with mapped volumes
    Given a VxFlexOS service
    And a valid snapshot consistency group
    When I call Probe
    And I call PublishVolume with "single-writer"
    And a valid PublishVolumeResponse is returned
    And I call DeleteSnapshot
    Then the error contains "One or more consistency group volumes are exposed and may be in use"

  Scenario: Delete snapshot consistency with induced remove volume error
    Given a VxFlexOS service
    And a valid snapshot consistency group
    And I induce error "RemoveVolumeError"
    When I call Probe
    And I call DeleteSnapshot
    Then the error contains "error removing snapshot"

  Scenario: Idempotent create a volume from a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I call Create Volume from Snapshot
    And no error was received
    And I call Create Volume from Snapshot
    Then a valid CreateVolumeResponse is returned
    And no error was received

  Scenario: Create a volume from a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I call Create Volume from Snapshot
    Then a valid CreateVolumeResponse is returned
    And no error was received

  Scenario: Create a volume from a snapshot with wrong capacity
    Given a VxFlexOS service
    And a valid snapshot
    And the wrong capacity
    When I call Probe
    And I call Create Volume from Snapshot
    Then the error contains "incompatible size"

  Scenario: Create a volume from a snapshot with wrong storage pool
    Given a VxFlexOS service
    And a valid snapshot
    And the wrong storage pool
    When I call Probe
    And I call Create Volume from Snapshot
    Then the error contains "different than the requested storage pool"

  Scenario: Create a volume from a snapshot with induced volume not found
    Given a VxFlexOS service
    And a valid snapshot
    And I induce error "GetVolByIDError"
    When I call Probe
    And I call Create Volume from Snapshot
    Then the error contains "Snapshot not found"

  Scenario: Create a volume from a snapshot with induced create snapshot error
    Given a VxFlexOS service
    And a valid snapshot
    And I induce error "CreateSnapshotError"
    When I call Probe
    And I call Create Volume from Snapshot
    Then the error contains "Failed to create snapshot"

  Scenario: Idempotent create a volume from a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I induce error <error>
    And I call Create Volume from Snapshot
    And a valid CreateVolumeResponse is returned
    And no error was received
    And I call Create Volume from Snapshot
    Then the error contains <errormsg>

    Examples:
      | error          | errormsg                                                                |
      | "none"         | "none"                                                                  |
      | "BadVolIDJSON" | "Failed to create vol from snap -- GetVolume returned unexpected error" |

  Scenario Outline: Call ControllerExpandVolume
    Given a VxFlexOS service
    And I call Probe
    And I call CreateVolumeSize "volume10" "32"
    And a valid CreateVolumeResponse is returned
    And I induce error <error>
    Then I call ControllerExpandVolume set to <GB>
    And the error contains <errmsg>
    And I call ControllerExpandVolume set to <GB>
    Then the error contains <errmsg>

    Examples:
      | error                | GB | errmsg                  |
      | "none"               | 32 | "none"                  |
      | "SetVolumeSizeError" | 64 | "induced error"         |
      | "none"               | 16 | "none"                  |
      | "NoVolumeIDError"    | 64 | "volume ID is required" |
      | "none"               | 64 | "none"                  |
      | "GetVolByIDError"    | 64 | "induced error"         |

  Scenario Outline: Call NodeExpandVolume with non sysID and no defaultSysID
    Given setup Get SystemID to fail
    And a VxFlexOS service
    And I call CreateVolumeSize "volume4" "32"
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    And I call NodePublishVolume "SDC_GUID"
    And I induce error "EmptySysIDInNodeExpand"
    When I call NodeExpandVolume with volumePath as "test/tmp/datadir"
    Then the error contains "systemID is not found in the request and there is no default system"

  Scenario Outline: Call NodeExpandVolume with invalid volID
    Given undo setup Get SystemID to fail
    And a VxFlexOS service
    And I call Probe
    And I call CreateVolumeSize "volume4" "32"
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    And I call NodePublishVolume "SDC_GUID"
    And no error was received
    And I induce error "WrongVolIDErrorInNodeExpand"
    When I call NodeExpandVolume with volumePath as "test/tmp/datadir"
    Then the error contains "not published to node"

  Scenario Outline: Call NodeExpandVolume
    Given a VxFlexOS service
    And I call Probe
    And I call CreateVolumeSize "volume4" "32"
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "xfs"
    And get Node Publish Volume Request
    And I call NodePublishVolume "SDC_GUID"
    And no error was received
    And I induce error <error>
    When I call NodeExpandVolume with volumePath as <volPath>
    Then the error contains <errormsg>

    Examples:
      | error                                   | volPath             | errormsg                                    |
      | "none"                                  | ""                  | "Volume path required"                      |
      | "none"                                  | "test/tmp/datadir"  | "none"                                      |
      | "GOFSInduceFSTypeError"                 | "test/tmp/datadir"  | "Failed to fetch filesystem"                |
      | "GOFSInduceResizeFSError"               | "test/tmp/datadir"  | "Failed to resize device"                   |
      | "NoVolumeIDError"                       | "test/tmp/datadir"  | "volume ID is required"                     |
      | "none"                                  | "not/a/path/1234"   | "Could not stat volume path"                |
      | "none"                                  | "test/tmp/datafile" | "none"                                      |
      | "CorrectFormatBadCsiVolIDInNodeExpand"  | "test/tmp/datadir"  | "is not configured in the driver"           |
      | "VolumeIDTooShortErrorInNodeExpand"     | "test/tmp/datadir"  | "is shorter than 3 chars, returning error"  |
      | "TooManyDashesVolIDInNodeExpand"        | "test/tmp/datadir"  | "is not configured in the driver"           |
  
  Scenario Outline: Call NodeGetVolumeStats with various errors
    Given a VxFlexOS service
    And a controller published volume
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    When I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I induce error <error> 
    And I call NodeGetVolumeStats
    Then the error contains <errormsg>
    And a correct NodeGetVolumeStats Response is returned
    
    Examples:
      | error                    | errormsg                   | 
      | "none"                   | "none"                     | 
      | "BadVolIDError"          | "id must be a hexadecimal" | 
      | "NoVolIDError"           | "no volume ID  provided"   |
      | "BadMountPathError"      | "none"                     | 
      | "NoMountPathError"       | "no volume Path provided"  | 
      | "NoVolIDSDCError"        | "none"                     |  
      | "GOFSMockGetMountsError" | "none"                     |
      | "NoVolError"             | "none"                     |
      | "NoSysNameError"         | "systemID is not found"    |

  Scenario: Call getSystemNameMatchingError, should get error in log but no error returned
    Given a VxFlexOS service
    When I call getSystemNameMatchingError
    Then no error was received

  Scenario: Call getSystemName, should get error Unable to probe system with ID
    Given a VxFlexOS service
    When I call getSystemNameError
    Then the error contains "missing VxFlexOS system name"

  Scenario: Call getSystemName, should get Found system Name: mocksystem
    Given a VxFlexOS service
    When I call getSystemName
    Then no error was received

  Scenario: Call New in service, a new service should return
    Given a VxFlexOS service
    When I call NewService
    Then a new service is returned

  Scenario: Call getVolProvisionType with bad params
    Given a VxFlexOS service
    When I call getVolProvisionType with bad params
    Then the error contains "getVolProvisionType - invalid boolean received"

  Scenario: Call getstoragepool with wrong ID
    Given a VxFlexOS service
    And I call Probe
    When i Call getStoragePoolnameByID "123"
    Then the error contains "cannot find storage pool"

  Scenario: Call Node getAllSystems
    Given a VxFlexOS service
    When I Call nodeGetAllSystems
    Then no error was received

  Scenario: Call Node getAllSystems
    Given a VxFlexOS service
    And I do not have a gateway connection
    And I do not have a valid gateway endpoint
    When I Call nodeGetAllSystems
    Then the error contains "missing VxFlexOS Gateway endpoint"

  Scenario: Call Node getAllSystems
    Given a VxFlexOS service
    And I do not have a gateway connection
    And I do not have a valid gateway password
    When I Call nodeGetAllSystems
    Then the error contains "missing VxFlexOS MDM password"

  Scenario: Call evalsymlinks
    Given a VxFlexOS service
    When I call evalsymlink "invalidpath"
    Then the error contains "Could not evaluate symlinks for path"

  Scenario: Idempotent clone of a volume
    Given a VxFlexOS service
    And I induce error <error>
    And I call CreateVolume "vol1"
    And a valid CreateVolumeResponse is returned
    And I call Clone volume
    And no error was received
    And I call Clone volume
    Then the error contains <errormsg>

    Examples:
      | error          | errormsg                                                        |
      | "none"         | "none"                                                          |
      | "BadVolIDJSON" | "Failed to create clone -- GetVolume returned unexpected error" |

  Scenario: Clone a volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call Clone volume
    Then a valid CreateVolumeResponse is returned
    And no error was received

  Scenario: Clone a volume with wrong capacity
    Given a VxFlexOS service
    And a valid volume
    And the wrong capacity
    When I call Probe
    And I call Clone volume
    Then the error contains "incompatible size"

  Scenario: Clone a volume with invalid volume
    Given a VxFlexOS service
    And an invalid volume
    When I call Probe
    And I call Clone volume
    Then the error contains "Volume not found"

  Scenario: Clone a volume with wrong storage pool
    Given a VxFlexOS service
    And a valid volume
    And the wrong storage pool
    When I call Probe
    And I call Clone volume
    Then the error contains "different from the requested storage pool"

  Scenario: Clone a volume with induced volume not found
    Given a VxFlexOS service
    And a valid volume
    And I induce error "CreateSnapshotError"
    When I call Probe
    And I call Clone volume
    Then the error contains "Failed to call CreateSnapshotConsistencyGroup to clone volume"

  Scenario: Test BeforeServe must run last
    Given a VxFlexOS service
    And I invalidate the Probe cache
    When I call BeforeServe
    # Get different error message on Windows vs. Linux
    Then the error contains "unable to login to VxFlexOS Gateway"

  Scenario: Test getArrayConfig with invalid config file
    Given an invalid config <configPath>
    When I call getArrayConfig
    Then the error contains <errorMsg>
    Examples:
      | configPath                                  | errorMsg                                                              |
      | "features/array-config/DO_NOT_EXIST"        | "does not exist"                                                      |
      | "features/array-config/unable_to_parse"     | "Unable to parse the credentials"                                     |
      | "features/array-config/zero_length"         | "no arrays are provided in vxflexos-creds secret"                     |
      | "features/array-config/duplicate_system_ID" | "duplicate system ID"                                                 |
      | "features/array-config/invalid_system_name" | "invalid value for system name"                                       |
      | "features/array-config/invalid_username"    | "invalid value for Username"                                          |
      | "features/array-config/invalid_password"    | "invalid value for Password"                                          |
      | "features/array-config/invalid_endpoint"    | "invalid value for Endpoint"                                          |
      | "features/array-config/two_default_array"   | "'isDefault' parameter presents more than once in storage array list" |
      | "features/array-config/empty"               | "arrays details are not provided in vxflexos-creds secret"            |

  Scenario: Call ControllerGetVolume good scenario
    Given a VxFlexOS service
    And I call Probe
    When I call ControllerGetVolume
    Then a valid ControllerGetVolumeResponse is returned
  
  Scenario: Call ControllerGetVolume bad scenario
    Given a VxFlexOS service
    And I call Probe
    And I induce error "NoVolumeIDError"
    When I call ControllerGetVolume
    Then the error contains "volume ID is required"
  
  
  Scenario: getProtectionDomainIDFromName, everything works
    Given a VxFlexOS service
    And I call Probe
    When I call getProtectionDomainIDFromName "15dbbf5617523655" "mocksystem"
    Then the error contains "none"

   
  Scenario: getProtectionDomainIDFromName, bad name
    Given a VxFlexOS service
    And I call Probe
    When I call getProtectionDomainIDFromName "15dbbf5617523655" "DoesNotExist"
    Then the error contains "Couldn't find protection domain"
 
   
  Scenario: getProtectionDomainIDFromName, no name provided
    Given a VxFlexOS service
    And I call Probe
    When I call getProtectionDomainIDFromName "15dbbf5617523655" ""
    Then the error contains "none"

  Scenario: getProtectionDomainIDFromName, bad systemID
    Given a VxFlexOS service
    And I call Probe
    And I induce error "WrongSysNameError"
    And I call getProtectionDomainIDFromName "15dbbf5617523655" "mocksystem"
    Then the error contains "systemid or systemname not found"

  Scenario: getArrayInstallationID, everything works
    Given a VxFlexOS service
    And I call Probe
    When I call getArrayInstallationID "15dbbf5617523655"
    Then the error contains "none"

  Scenario: getArrayInstallationID, bad systemID
    Given a VxFlexOS service
    And I call Probe
    And I induce error "WrongSysNameError"
    When I call getArrayInstallationID "15dbbf5617523655"
    Then the error contains "systemid or systemname not found"

  Scenario: Call for setting QoS parameters, everything works
    Given a VxFlexOS service
    And I call Probe
    When I call setQoSParameters with systemID "15dbbf5617523655" sdcID "d0f055a700000000" bandwidthLimit "10240" iopsLimit "11" volumeName "k8s-a031818af5" csiVolID "15dbbf5617523655-456ca4fc00000009" nodeID "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
    Then the error contains "none"

  Scenario: Call for setting QoS parameters, invalid bandwidthLimit
    Given a VxFlexOS service
    And I induce error "SDCLimitsError"
    When I call Probe
    And I call setQoSParameters with systemID "15dbbf5617523655" sdcID "d0f055a700000000" bandwidthLimit "1023" iopsLimit "11" volumeName "k8s-a031818af5" csiVolID "15dbbf5617523655-456ca4fc00000009" nodeID "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
    Then the error contains "error setting QoS parameters"

  Scenario: Call for setting QoS parameters, invalid iopsLimit
    Given a VxFlexOS service
    And I induce error "SDCLimitsError"
    When I call Probe
    And I call setQoSParameters with systemID "15dbbf5617523655" sdcID "d0f055a700000000" bandwidthLimit "10240" iopsLimit "10" volumeName "k8s-a031818af5" csiVolID "15dbbf5617523655-456ca4fc00000009" nodeID "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
    Then the error contains "error setting QoS parameters"

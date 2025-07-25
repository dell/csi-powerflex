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
      | "15dbbf5617523655"             | "none"   |


  Scenario: Identity GetPluginInfo good call
    Given a VxFlexOS service
    When I call GetPluginInfo
    When I call BeforeServe
    Then configMap is updated
    Then a valid GetPlugInfoResponse is returned

  Scenario Outline: Identity GetPluginInfo bad call
    Given a VxFlexOS service
    When I call GetPluginInfo
    And I induce error <error>
    When I call BeforeServe
    Then configMap is updated
    Then a valid GetPlugInfoResponse is returned
    Examples:
      | error                           |
      | "UpdateConfigMapUnmarshalError" |
      | "GetIPAddressByInterfaceError"  |
      | "UpdateConfigK8sClientError"    |
      | "UpdateConfigFormatError"       |
      | "ConfigMapNotFoundError"        |

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
      | "a/b"    | ""       |

  Scenario Outline: multi array getFilesystemIDFromCsiVolumeID for NFS volumes with different examples
    Given a VxFlexOS service
    And I call getFilesystemIDFromCsiVolumeID <csiVolID>
    Then the fileSystemID is <fsID>
    Examples:
      | csiVolID        | fsID            |
      | "abcd/nfs123"   | "nfs123"        |
      | "badcsiVolID"   | ""              |
      |  ""             | ""              |

  Scenario Outline: multi array getSystemIDFromCsiVolumeID for NFS volumes with different examples
    Given a VxFlexOS service
    And I call getSystemIDFromCsiVolumeIDNfs <csiVolID>
    Then the systemID is <systemID>
    Examples:
      | csiVolID           | systemID |
      | "abcd/nfs123"      | "abcd"   |
      | "badSystemID"      | ""       |
      |  ""                | ""       |

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
    Then the error contains "unable to login to PowerFlex Gateway"

  Scenario Outline: Probe Call with various errors
    Given a VxFlexOS service
    And I induce error <error>
    When I invalidate the Probe cache
    And I call Probe
    Then the error contains <msg>

    Examples:
      | error               | msg                                            |
      | "NoEndpointError"   | "missing PowerFlex Gateway endpoint"            |
      | "NoUserError"       | "missing PowerFlex MDM user"                    |
      | "NoPasswordError"   | "missing PowerFlex MDM password"                |
      | "NoSysNameError"    | "missing PowerFlex system name"                 |
      | "WrongSysNameError" | "unable to find matching PowerFlex system name" |
      | "WrongSystemIDError"| "systemid or systemname not found"              |


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



  Scenario Outline: Create volume with Accessiblity Requirements NFS volumes Invalid topology error
    Given a VxFlexOS service
    When I call Probe
    And I specify bad NFS AccessibilityRequirements with a SystemID of <sysID>
    And I call CreateVolume "volume1"
    Then the error contains "Invalid topology requested for NFS Volume"
    Examples:
      | sysID                      |
      | "f.service.opt.SystemName" |



  Scenario Outline: Create volume with Accessibility Requirements for NFS volumes with different examples
    Given a VxFlexOS service
    When I call Probe
    And I specify NFS AccessibilityRequirements with a SystemID of <sysID>
    And I call CreateVolume "volume1"
    Then the error contains <errormsg>

    Examples:
      | sysID                      | errormsg                               |
      | "f.service.opt.SystemName" | "none"                                 |
      | ""                         | "is not accessible based on Preferred" |
      | "Unknown"                  | "is not accessible based on Preferred" |
      | "badSystem"                | "is not accessible based on Preferred" |

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


  Scenario: Create mount volume NFS no error
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned


  Scenario: Create Volume with invalid probe cache, no endpoint, and no admin NFS system ID not found error
    Given a VxFlexOS service
    When I induce error "NoAdminError"
    And I induce error "NoEndpointError"
    And I invalidate the Probe cache
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then the error contains "No system ID is found in parameters or as default"


  Scenario: Create mount volume NFS nas server not found error
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I induce error "NasNotFoundError"
    And I call CreateVolume "volume1"
    Then the error contains "nas server not found"


  Scenario: Idempotent create mount volume NFS storage pool not found error
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume4"
    When I specify CreateVolumeMountRequest "nfs"
    And I change the StoragePool "no_storage_pool"
    And I call CreateVolume "volume4"
    Then the error contains "Couldn't find storage pool"




  Scenario: Create mount volume NFS with NoAdmin
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I induce error "NoAdminError"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create mount volume idempotent test
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "xfs"
    And I call CreateVolume "volume2"
    And I call CreateVolume "volume2"
    Then a valid CreateVolumeResponse is returned


  Scenario: Create mount volume idempotent NFS no error
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume2"
    And I call CreateVolume "volume2"
    Then a valid CreateVolumeResponse is returned


  Scenario: Create mount volume with bad capacity NFS bad capacity error
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I specify a BadCapacity
    And I induce error "BadCapacityError"
    And I call CreateVolume "bad capacity"
    Then the error contains "bad capacity"


  Scenario: Idempotent create mount volume with different sizes NFS different size error
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolumeSize nfs "volume3" "8"
    And I call CreateVolumeSize nfs "volume3" "16"
    Then the error contains "'Volume name' already exists and size is different"

  Scenario: Call NodeGetInfo with invalid MaxVolumesPerNode
    Given a VxFlexOS service
    And an invalid MaxVolumesPerNode
    When I call NodeGetInfo
    Then the error contains "maxVxflexosVolumesPerNode MUST NOT be set to negative value"

  Scenario: Call GetNodeLabels with valid labels
    Given a VxFlexOS service
    When I call GetNodeLabels
    Then a valid label is returned

  Scenario: Call GetNodeLabels with invalid node
    Given a VxFlexOS service
    When I call GetNodeLabels with invalid node
    Then the error contains "Unable to fetch the node labels"

  Scenario: Call GetNodeUID with invalid node
    Given a VxFlexOS service
    When I call GetNodeUID with invalid node
    Then the error contains "Unable to fetch the node details"

  Scenario: Call NodeGetInfo with invalid volume limit node labels
    Given a VxFlexOS service
    When I call NodeGetInfo with invalid volume limit node labels
    Then the error contains "invalid value"

  Scenario: Call NodeGetInfo with valid volume limit node labels
    Given a VxFlexOS service
    When I call NodeGetInfo with valid volume limit node labels
    Then the Volume limit is set

  Scenario: Call NodeGetInfo and validate Node UID
    Given a VxFlexOS service
    When I call NodeGetInfo with a valid Node UID
    Then a valid NodeGetInfoResponse with node UID is returned

  Scenario: Call GetNodeUID
    Given a VxFlexOS service
    When I call GetNodeUID
    Then a valid node uid is returned

  Scenario: Call ParseInt64FromContext to validate EnvMaxVolumesPerNode
    Given a VxFlexOS service
    When I set invalid EnvMaxVolumesPerNode
    Then the error contains "invalid int64 value"

  Scenario: Call GetNodeLabels with invalid KubernetesClient
    Given a VxFlexOS service
    When I call GetNodeLabels with unset KubernetesClient
    Then the error contains "init client failed with error"

  Scenario: Call GetNodeUID with invalid KubernetesClient
    Given a VxFlexOS service
    When I call GetNodeUID with unset KubernetesClient
    Then the error contains "init client failed with error"

  Scenario: Call GetCapacity without specifying Storage Pool Name (this returns overall capacity)
    Given a VxFlexOS service
    When I call Probe
    And I call GetCapacity with storage pool ""

  Scenario: Call GetCapacity for a system using Availability zones
    Given a VxFlexOS service
    And I use config <config>
    When I call Probe
    And I call GetCapacity with Availability Zone <zone-key> <zone-name>
    Then the error contains <errorMsg>

    Examples:
      | config             | zone-key                         | zone-name   | errorMsg                                              |
      | "multi_az"         | "zone.csi-vxflexos.dellemc.com"  | "zoneA"     | "none"                                                |
      | "multi_az"         | "zone.csi-vxflexos.dellemc.com"  | "badZone"   | "could not find an array assigned to zone 'badZone'"  |

  Scenario: Call GetCapacity with valid Storage Pool Name
    Given a VxFlexOS service
    When I call Probe
    And I call GetCapacity with storage pool "viki_pool_HDD_20181031"
    Then a valid GetCapacityResponse is returned

  Scenario: Call GetMaximumVolumeSize with Systemid
    Given a VxFlexOS service
    When I call Probe
    And I call CreateVolume "volume123"
    Then a valid CreateVolumeResponse is returned
    And I call GetCapacity with storage pool "viki_pool_HDD_20181031"
    And I call get GetMaximumVolumeSize with systemid "14dbbf5617523654"
    Then a valid GetCapacityResponse1 is returned

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

  Scenario: Snapshot a single fileSystem Volume
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received

  Scenario: Idempotent Snapshot a single fileSystem Volume
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    And I call CreateSnapshot NFS "snap1"
    And no error was received

  Scenario: Snapshot a single fileSystem Volume large name
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap-3m7xvJ-5dT4sPqfzY1Mv9KaZXc2Wb9A-"
    And no error was received

  Scenario: Request to create NFS Snapshot with same name and different SourceVolumeID
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    And I call CreateVolume "A Different Volume"
    And a valid CreateVolumeResponse is returned
    And I induce error "WrongFileSystemIDError"
    And I call CreateSnapshot NFS "snap1"
    Then the error contains " code = AlreadyExists desc = snapshot with name 'snap1' exists"

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


  Scenario: Call snapshot create with GetFileSystemsByIdError
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "GetFileSystemsByIdError"
    And I call CreateSnapshot NFS "snap1"
    Then the error contains "rpc error: code = NotFound desc = NFS volume 766f6c756d6531 not found"


  Scenario: Call snapshot create but recieve create snapshot error
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "CreateSnapshotsError"
    And I call CreateSnapshot NFS "snap1"
    Then the error contains "error creating snapshot with name"


  Scenario: Call snapshot create but recieve snapshot with id not found error
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "GetSnashotByIdError"
    And I call CreateSnapshot NFS "snap1"
    Then the error contains " snapshot with ID 736e617031 was not found"


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

  Scenario: Delete a NFS snapshot no error
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I call DeleteSnapshot NFS
    Then no error was received

  Scenario: Idempotent delete a snapshot
    Given a VxFlexOS service
    And a valid snapshot
    When I call Probe
    And I call DeleteSnapshot
    Then no error was received
    And I call DeleteSnapshot
    Then no error was received

  Scenario: Idempotent delete a NFS snapshot no error
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I call DeleteSnapshot NFS
    Then no error was received
    And I call DeleteSnapshot NFS
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

  Scenario: Delete a NFS snapshot delete snapshot error
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I induce error "DeleteSnapshotError"
    And I call DeleteSnapshot NFS
    Then the error contains "error while deleting the filesystem snapshot"

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

  Scenario: Create a volume from a snapshot NFS no error
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I call Create Volume from SnapshotNFS
    Then a valid CreateVolumeResponse is returned
    And no error was received

  Scenario: Idempotent create a volume from a snapshot no error
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I call Create Volume from SnapshotNFS
    Then a valid CreateVolumeResponse is returned
    And no error was received
    When I call Probe
    And I call Create Volume from SnapshotNFS
    Then a valid CreateVolumeResponse is returned
    And no error was received

  Scenario: Create a volume from a snapshot NFS snapshot not found error
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I induce error "GetFileSystemsByIdError"
    And I call Create Volume from SnapshotNFS
    Then the error contains "Snapshot not found"

  Scenario: Create a volume from a snapshot NFS incompatible size error
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    And the wrong capacity
    When I call Probe
    And I call Create Volume from SnapshotNFS
    Then the error contains "incompatible size"

  Scenario: Create a volume from a snapshot NFS different storage pool error
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    And the wrong storage pool
    When I call Probe
    And I call Create Volume from SnapshotNFS
    Then the error contains "different than the requested storage pool"

  Scenario: Create a volume from a snapshot NFS restoreVolumeError
    Given a VxFlexOS service
    And I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    When I call Probe
    And I induce error "restoreVolumeError"
    And I call Create Volume from SnapshotNFS
    Then the error contains "error during fs creation from snapshot"

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
    When I call NodeExpandVolume with volumePath as "test/00000000-1111-0000-0000-000000000000/datadir"
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
    When I call NodeExpandVolume with volumePath as "test/00000000-1111-0000-0000-000000000000/datadir"
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
      | "none"                                 | "test/00000000-1111-0000-0000-000000000000/datadir"  | "none"                                     |
      | "GOFSInduceFSTypeError"                | "test/00000000-1111-0000-0000-000000000000/datadir"  | "Failed to fetch filesystem"               |
      | "GOFSInduceResizeFSError"              | "test/00000000-1111-0000-0000-000000000000/datadir"  | "Failed to resize device"                  |
      | "NoVolumeIDError"                      | "test/00000000-1111-0000-0000-000000000000/datadir"  | "volume ID is required"                    |
      | "none"                                 | "test/nonexistent/target/path"                       | "Could not stat volume path"               |
      | "none"                                 | "test/00000000-1111-0000-0000-000000000000/datafile" | "none"                                     |
      | "CorrectFormatBadCsiVolIDInNodeExpand" | "test/00000000-1111-0000-0000-000000000000/datadir"  | "is not configured in the driver"          |
      | "VolumeIDTooShortErrorInNodeExpand"    | "test/00000000-1111-0000-0000-000000000000/datadir"  | "is shorter than 3 chars, returning error" |
      | "TooManyDashesVolIDInNodeExpand"       | "test/00000000-1111-0000-0000-000000000000/datadir"  | "is not configured in the driver"          |

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
      | error                    | errormsg                          |
      | "none"                   | "none"                            |
      | "BadVolIDError"          | "id must be a hexadecimal"        |
      | "NoVolIDError"           | "no volume ID  provided"          |
      | "BadMountPathError"      | "none"                            |
      | "NoMountPathError"       | "no volume Path provided"         |
      | "NoVolIDSDCError"        | "none"                            |
      | "GOFSMockGetMountsError" | "none"                            |
      | "NoVolError"             | "none"                            |
      | "NoSysNameError"         | "systemID is not found"           |
      | "WrongSystemError"       | "is not configured in the driver" |

  Scenario: Call getSystemNameMatchingError, should get error in log but no error returned
    Given a VxFlexOS service
    When I call getSystemNameMatchingError
    Then no error was received

  Scenario: Call getSystemName, should get error Unable to probe system with ID
    Given a VxFlexOS service
    When I call getSystemNameError
    Then the error contains "missing PowerFlex system name"

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
    Then the error contains "missing PowerFlex Gateway endpoint"

  Scenario: Call Node getAllSystems
    Given a VxFlexOS service
    And I do not have a gateway connection
    And I do not have a valid gateway password
    When I Call nodeGetAllSystems
    Then the error contains "missing PowerFlex MDM password"

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
    Then the error contains "unable to login to PowerFlex Gateway"

  Scenario: Test getArrayConfig with invalid config file
    Given an invalid config <configPath>
    When I call getArrayConfig
    Then the error contains <errorMsg>
    Examples:
      | configPath                                  | errorMsg                                                              |
      | "features/array-config/DO_NOT_EXIST"        | "does not exist"                                                      |
      | "features/array-config/unable_to_parse"     | "unable to parse the credentials"                                     |
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

  Scenario: Call probe for renaming SDC with prefix
    Given a VxFlexOS service
    And I set renameSDC with renameEnabled "true" prefix "t"
    And I call Probe
    When I call Node Probe
    Then the error contains "none"

  Scenario: Call probe for renaming SDC without prefix
    Given a VxFlexOS service
    And I set renameSDC with renameEnabled "true" prefix ""
    And I call Probe
    When I call Node Probe
    Then the error contains "none"

  Scenario: Call probe for renaming SDC, renameSdc error
    Given a VxFlexOS service
    And I induce error "SetSdcNameError"
    And I set renameSDC with renameEnabled "true" prefix "t"
    And I call Probe
    When I call Node Probe
    Then the error contains "Failed to rename SDC"

  Scenario: Call probe for renaming SDC, sdc error
    Given a VxFlexOS service
    And I induce error "GetSdcInstancesError"
    And I set renameSDC with renameEnabled "true" prefix "t"
    And I call Probe
    When I call Node Probe
    Then the error contains "induced error"

  Scenario: Call Probe for approving sdc
    Given a VxFlexOS service
    And I set approveSDC with approveSDCEnabled "true"
    And I call Probe
    When I call Node Probe
    Then the error contains "none"

  Scenario: Call Probe for approving sdc, invalid guid
    Given a VxFlexOS service
    And I induce error "ApproveSdcError"
    And I set approveSDC with approveSDCEnabled "true"
    And I call Probe
    When I call Node Probe
    Then the error contains "The given GUID is invalid"

  Scenario: Controller expand volume for NFS
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And a controller published volume
    When I call ControllerExpandVolume set to "10"
    Then no error was received

  Scenario: Controller shrink volume for NFS
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "16"
    And a controller published volume
    When I call ControllerExpandVolume set to "8"
    Then no error was received

  Scenario: Controller expand volume for NFS - idempotent case
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    And a controller published volume
    When I call ControllerExpandVolume set to "10"
    Then no error was received

  Scenario: Controller expand volume for NFS - incorrect system name
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    And a controller published volume
    And I induce error "WrongSysNameError"
    When I call ControllerExpandVolume set to "16"
    Then the error contains "failure to load volume"

  Scenario: Call ControllerExpandVolume for NFS - volume ID not found
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    And a controller published volume
    And I induce error "NoVolumeIDError"
    Then I call ControllerExpandVolume set to "16"
    And the error contains "volume ID is required"

  Scenario: Controller expand volume for NFS with quota enabled
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then no error was received

  Scenario: Controller expand volume for NFS with quota disabled
    Given a VxFlexOS service
    And I disable quota for filesystem
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then no error was received

  Scenario: Controller expand volume for NFS with quota enabled, modify filesystem error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And I induce error "ModifyFSError"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then the error contains "Modify filesystem failed with error:"

  Scenario: Controller expand volume for NFS with quota enabled, modify quota error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And I induce error "ModifyQuotaError"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then the error contains "Modifying tree quota for filesystem failed, error:"

  Scenario: Controller expand volume for NFS with quota enabled, GetFileSystemsByIdError
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And I induce error "GetFileSystemsByIdError"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then the error contains "rpc error: code = NotFound desc = volume"

  Scenario: Controller expand volume for NFS with quota enabled, GetQuotaByFSIDError
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    And I induce error "GetQuotaByFSIDError"
    And a controller published volume
    When I call ControllerExpandVolume set to "12"
    Then the error contains "Fetching tree quota for filesystem failed, error:"

  Scenario: Parse valid IP
    When I call ParseCIDR with ip "127.0.0.1"
    And no error was received

  Scenario: Parse invalid IP
    When I call ParseCIDR with ip "127.0.0"
    Then the error contains "invalid CIDR address"

  Scenario: Get IP with valid IP and valid Mask
    When I call GetIPListWithMaskFromString with ip "127.0.0.1/32"
    And no error was received

  Scenario: Get IP with invalid IP and invalid Mask
    When I call GetIPListWithMaskFromString with ip "127.0.1/34"
    Then the error contains "doesn't seem to be a valid IP"

  Scenario: Get IP with valid IP and invalid Mask
    When I call GetIPListWithMaskFromString with ip "127.0.1.1/34"
    Then the error contains "doesn't seem to be a valid IP"

  Scenario: Get IP with invalid Mask
    When I call GetIPListWithMaskFromString with ip "127.0.1.1//34"
    Then the error contains "doesn't seem to be a valid IP"

  Scenario: Parse IP with no Mask
    When I call parseMask with ip "192.168.1.34"
    Then the error contains "parse mask: error parsing mask"

  Scenario: External Access Already Added
    Given an NFSExport instance with nfsexporthost <nfsexporthost>
    When I call externalAccessAlreadyAdded with externalAccess <externalAccess>
    Then the error contains <errorMsg>
    Examples:
      |  nfsexporthost                  | externalAccess                | errorMsg                              |
      |  "127.0.0.1/255.255.255.255"    | "127.0.0.1/255.255.255.255"   | "external access exists"              |
      |  "127.1.1.0/255.255.255.255"    | "127.0.0.1/255.255.255.255"   | "external access does not exist"      |

  Scenario: Get NAS server id from name
    Given a VxFlexOS service
    And I call Probe
    When I call Get NAS server from name <systemid> <nasservername>
    And I induce error <error>
    Then the error contains <errorMsg>
    Examples:
      |  systemid                  | nasservername                      |   error               |  errorMsg                                   |
      |  "15dbbf5617523655"        | "dummy-nas-server"                 |   ""                  |  "none"                                     |
      |  "15dbbf5617523655"        | "invalid-nas-server-id"            |   "NasNotFoundError"  |  "could not find given NAS server by name"  |
      |  "15dbbf5617523655"        | ""                                 |   ""                  |  "NAS server not provided"                  |

  Scenario: Check NFS enabled on Array
    Given a VxFlexOS service
    And I call Probe
    And I induce error <error>
    When I call check NFS enabled <systemid> <nasserver>
    Then the error contains <errorMsg>
    Examples:
      |  systemid                  | nasserver                                |   error                         |  errorMsg                   |
      |  "15dbbf5617523655"        | "63ec8e0d-4551-29a7-e79c-b202f2b914f3"   |   ""                            | "none"                      |

  Scenario: Create Volume for multi-available zone
    Given a VxFlexOS service
    And I use config <config>
    When I call Probe
    And I call CreateVolume <name> with zones
    Then the error contains <errorMsg>
    Examples:
      | name      | config             | errorMsg                                               |
      | "volume1" | "multi_az"         | "none"                                                 |
      | "volume1" | "invalid_multi_az" | "no zone topology found in accessibility requirements" |

  Scenario: Call NodeGetInfo without zone label
    Given a VxFlexOS service
    And I use config "config"
    When I call NodeGetInfo
    Then a NodeGetInfo is returned without zone topology

  Scenario: Call NodeGetInfo with zone label
    Given a VxFlexOS service
    And I use config <config>
    When I call NodeGetInfo with zone labels
    Then a valid NodeGetInfo is returned with node topology
    Examples:
      | config             |
      | "multi_az"         |
      | "multi_az_custom_labels" |

  Scenario: Snapshot a single volume in zone
    Given a VxFlexOS service
    And I use config <config>
    When I call Probe
    And I call CreateVolume "volume1" with zones
    And a valid CreateVolumeResponse is returned
    And I call CreateSnapshot <name>
    Then a valid CreateSnapshotResponse is returned
    And I call Create Volume for zones from Snapshot <name>
    Then a valid CreateVolumeResponse is returned
    Examples:
      | name      | config             | errorMsg       |
      | "snap1"   | "multi_az"         | "none"         |

  Scenario: Clone a single volume in zone
    Given a VxFlexOS service
    And I use config <config>
    When I call Probe
    And I call CreateVolume <name> with zones
    And a valid CreateVolumeResponse is returned
    And I call Clone volume for zones <name>
    Then a valid CreateVolumeResponse is returned
    Examples:
      | name      | config             | errorMsg       |
      | "volume1"   | "multi_az"       | "none"         |

  Scenario: Probe all systems using availability zones
    Given a VxFlexOS service
    And I use config <config>
    When I call systemProbeAll in mode <mode>
    Then the error contains <errorMsg>
    Examples:
      | config      | mode          | errorMsg  |
      | "multi_az"  | "node"        | "none"    |
      | "multi_az"  | "controller"  | "none"    |

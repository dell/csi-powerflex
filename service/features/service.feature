Feature: VxFlex OS CSI interface
    As a consumer of the CSI interface
    I want to test service methods
    So that they are known to work

    Scenario: Identity GetPluginInfo good call
      Given a VxFlexOS service
      When I call GetPluginInfo
      Then a valid GetPlugInfoResponse is returned
    Scenario: Identity GetPluginCapabilitiles good call
      Given a VxFlexOS service
      When I call GetPluginCapabilities
      Then a valid GetPluginCapabilitiesResponse is returned
      
    Scenario: Identity Probe good call
      Given a VxFlexOS service
      When I call Probe
      Then a valid ProbeResponse is returned

@wip
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
| error               |  msg                                             |
| "NoEndpointError"   |  "missing VxFlexOS Gateway endpoint"             |
| "NoUserError"       |  "missing VxFlexOS MDM user"                     |
| "NoPasswordError"   |  "missing VxFlexOS MDM password"                 |
| "NoSysNameError"    |  "missing VxFlexOS system name"                  |
| "WrongSysNameError" |  "unable to find matching VxFlexOS system name"  |


# This injected error fails on Windows with no SDC but passes on Linux with SDC
     Scenario: Identity Probe call node probe Lsmod error
      Given a VxFlexOS service
      And there is a Node Probe Lsmod error
      When I invalidate the Probe cache
      And I call Probe
      Then the possible error contains "scini kernel module not loaded"

# This injected error fails on Windows with no SDC but passes on Linux with SDC
     Scenario: Identity Probe call node probe SdcGUID error
      Given a VxFlexOS service
      And there is a Node Probe SdcGUID error
      When I call Probe
      Then the possible error contains "unable to get SDC GUID"

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
      Then the error contains "failed to probe/init plugin:"
   

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

     Scenario: Create volume with Accessibility Requirements
      Given a VxFlexOS service
      When I call Probe
      And I specify AccessibilityRequirements
      And I call CreateVolume "accessibility"
      Then the error contains "AccessibilityRequirements is not currently supported"

     Scenario: Create volume with VolumeContentSource
      Given a VxFlexOS service
      When I call Probe
      And I specify VolumeContentSource
      And I call CreateVolume "volumecontentsource"
      Then the error contains "Volume as a VolumeContentSource is not supported"

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

     Scenario: Call NodeGetInfo which requires probe and returns error
      Given a VxFlexOS service
      And I induce error "require-probe"
      When I call NodeGetInfo
      Then the error contains "Node Service has not been probed"

     Scenario: Call GetCapacity without specifying Storage Pool Name (this returns overall capacity)
      Given a VxFlexOS service
      When I call Probe
      And I call GetCapacity with storage pool ""
      Then a valid GetCapacityResponse is returned

     Scenario: Call GetCapacity with valid Storage Pool Name
      Given a VxFlexOS service
      When I call Probe
      And I call GetCapacity with storage pool "viki_pool_HDD_20181031"
      Then a valid GetCapacityResponse is returned

     Scenario: Call GetCapacity without specifying Storage Pool and without probe
      Given a VxFlexOS service
      When I invalidate the Probe cache
      And I call GetCapacity with storage pool ""
      Then the error contains "Controller Service has not been probed"
	
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

     Scenario: Call ControllerGetCapabilities
      Given a VxFlexOS service
      When I call ControllerGetCapabilities
      Then a valid ControllerGetCapabilitiesResponse is returned

     Scenario Outline: Calls to validate volume capabilities
      Given a VxFlexOS service
      When I call Probe
      And I call CreateVolume "volume1"
      And a valid CreateVolumeResponse is returned
      And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
      Then the error contains <errormsg>

      Examples:
      | voltype    | access                     | fstype    | errormsg                                                          |
      | "block"    | "single-writer"            | "none"    | "none"                                                            |
      | "block"    | "multi-reader"             | "none"    | "none"                                                            |
      | "mount"    | "multi-writer"             | "ext4"    | "multi-node with writer(s) only supported for block access type"  |
      | "mount"    | "multi-node-single-writer" | "ext4"    | "multi-node with writer(s) only supported for block access type"  |
      | "mount"    | "unknown"                  | "ext4"    | "access mode cannot be UNKNOWN"                                   |
      | "none "    | "unknown"                  | "ext4"    | "unknown access type is not Block or Mount"                       |

     Scenario Outline: Call validate volume capabilities with non-existent volume
      Given a VxFlexOS service
      When I call Probe
      And an invalid volume
      And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
      Then the error contains <errormsg>

      Examples:
      | voltype       | access               | fstype      | errormsg                                                          |
      | "block"       | "single-writer"      | "none"      | "volume not found"                                                |

     Scenario Outline: Call with no probe volume to validate volume capabilities
      Given a VxFlexOS service
      When I invalidate the Probe cache
      And I call ValidateVolumeCapabilities with voltype <voltype> access <access> fstype <fstype>
      Then the error contains <errormsg>

      Examples:
      | voltype       | access               | fstype      | errormsg                                                          |
      | "block"       | "single-writer"      | "none"      | "Service has not been probed"                                     |

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

     Scenario: Call NodeUnstageVolume, should get unimplemented
      Given a VxFlexOS service
      And I call Probe
      When I call NodeUnstageVolume
      Then the error contains "Unimplemented"

     Scenario: Call NodeGetCapabilities should return a valid response
      Given a VxFlexOS service
      And I call Probe
      When I call NodeGetCapabilities
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
      And a valid CreateVolumeResponse is returned
      And I call CreateSnapshot "snapshot-a5d67905-14e9-11e9-ab1c-005056264ad3"
      And no error was received
      And I call CreateSnapshot "snapshot-a5d67905-14e9-11e9-ab1c-005056264ad3"
      Then a valid CreateSnapshotResponse is returned
      And no error was received

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
      Then the error contains "Controller Service has not been probed"

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
      Then the error contains "Controller Service has not been probed"

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
      And I call Create Volume from Snapshot
      And a valid CreateVolumeResponse is returned
      And no error was received
      Then I call Create Volume from Snapshot
      And no error was received
      And a valid CreateVolumeResponse is returned

    Scenario: Call ControllerExpandVolume, should get unimplemented
     Given a VxFlexOS service
     When I call ControllerExpandVolume
     Then the error contains "Unimplemented"

    Scenario: Call NodeExpandVolume, should get unimplemented
     Given a VxFlexOS service
     When I call NodeExpandVolume
     Then the error contains "Unimplemented"

    Scenario: Call NodeGetVolumeStats, should get unimplemented
     Given a VxFlexOS service
     When I call NodeGetVolumeStats
     Then the error contains "Unimplemented"




@wip
     Scenario: Test BeforeServe
      Given a VxFlexOS service
      And I invalidate the Probe cache
      When I call BeforeServe
      # Get different error message on Windows vs. Linux
      Then the error contains "Unable to initialize cert pool from system@@unable to login to VxFlexOS Gateway@@unable to get SDC GUID"

  
    
      

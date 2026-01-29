Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test controller publish / unpublish interfaces
  So that they are known to work

  Scenario: Publish volume with single writer
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    Then a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 1
    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: a Basic NFS controller Publish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with <access>
    Then a valid PublishVolumeResponse is returned
    Examples:
      | access                      |
      | "single-writer"             |
      | "single-node-single-writer" |
      | "single-node-multi-writer"  |
      | "multiple-reader"           |
      | "multiple-writer"           |
    
  Scenario: a Basic NFS controller Publish and unpublish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call UnpublishVolume nfs
    And no error was received
    Then a valid UnpublishVolumeResponse is returned

  Scenario: a Basic NFS controller Publish and unpublish no error with SDC dependency
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce SDC dependency
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call UnpublishVolume nfs
    And no error was received
    Then a valid UnpublishVolumeResponse is returned
    
    Scenario: a Basic NFS controller Publish and unpublish NFS export not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I induce error "nfsExportNotFoundError"
    And I call UnpublishVolume nfs
    Then the error contains "Could not find NFS Export"
    
    Scenario: a Basic NFS controller Publish and unpublish get NFS exports error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I induce error "NFSExportsInstancesError"
    And I call UnpublishVolume nfs
    Then the error contains "error getting the NFS Exports"
    
    Scenario: a Basic NFS controller Publish and unpublish modify NFS export error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I induce error "nfsExportModifyError"
    And I call UnpublishVolume nfs
    Then the error contains "Allocating host access failed"
    
    Scenario: a Basic NFS controller Publish and unpublish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I induce error "readHostsIncompatible"
    And I call UnpublishVolume nfs
    Then the error contains "none"
    
    Scenario: a Basic NFS controller Publish and unpublish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I induce error "writeHostsIncompatible"
    And I call UnpublishVolume nfs
    Then the error contains "none"
    
    Scenario: a Basic NFS controller Publish Idempotent no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    
    Scenario: a Basic NFS controller Publish Idempotent no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "single-node-multi-writer"
    Then a valid PublishVolumeResponse is returned

    Scenario: a Basic NFS controller Publish incompatible access mode error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "readHostsIncompatible"
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "with incompatible access mode"
    
    Scenario: a Basic NFS controller Publish incompatible access mode error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "writeHostsIncompatible"
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "with incompatible access mode"
    
    Scenario: a Basic NFS controller Publish incompatible access mode error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then the error contains "with incompatible access mode"
    
    Scenario: a Basic NFS controller Publish Idempotent no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    
    Scenario: a Basic NFS controller Publish incompatible access mode error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "with incompatible access mode"
  
  Scenario: a Basic NFS controller Publish and unpublish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I call UnpublishVolume nfs
    And no error was received
    Then a valid UnpublishVolumeResponse is returned
    
    Scenario: a Basic NFS controller Publish and unpublish no error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    And I call NFS PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    And I call UnpublishVolume nfs
    And no error was received
    Then a valid UnpublishVolumeResponse is returned
    
     Scenario: a Basic NFS controller Publish NFS export create error
      Given a VxFlexOS service
      When I specify CreateVolumeMountRequest "nfs"
      And I call CreateVolume "volume1"
      Then a valid CreateVolumeResponse is returned
      And I induce error "nfsExportError"
      And I call NFS PublishVolume with "single-writer"
      Then the error contains "create NFS Export failed"
      
      Scenario: a Basic NFS controller Publish modify NFS export error
      Given a VxFlexOS service
      When I specify CreateVolumeMountRequest "nfs"
      And I call CreateVolume "volume1"
      Then a valid CreateVolumeResponse is returned
      And I induce error "nfsExportModifyError"
      And I call NFS PublishVolume with "single-writer"
      Then the error contains "Allocating host access failed"
      
      Scenario: a Basic NFS controller Publish modify NFS export error
      Given a VxFlexOS service
      When I specify CreateVolumeMountRequest "nfs"
      And I call CreateVolume "volume1"
      Then a valid CreateVolumeResponse is returned
      And I induce error "nfsExportModifyError"
      And I call NFS PublishVolume with "multiple-reader"
      Then the error contains "Allocating host access failed"
  
  Scenario: a Basic NFS controller Publish failure to check volume status error
   Given a VxFlexOS service
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "failure checking volume status before controller"
   
   Scenario: a Basic NFS controller Publish volume not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I set bad FileSystem Id
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "volume not found"
    
    Scenario: a Basic NFS controller Publish NFS export not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "nfsExportNotFoundError"
    And I call NFS PublishVolume with "single-writer"
    Then the error contains "Could not find NFS Export"
   
  Scenario: a Basic NFS controller Publish and unpublish volume not found error
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
    And I set bad FileSystem Id
    And I call UnpublishVolume nfs
    Then the error contains "volume not found"
   
  Scenario: Publish legacy volume that is on non default array
    Given a VxFlexOS service
    And I induce error "LegacyVolumeConflictError"
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    Then the error contains "expecting this volume id only on default system. aborting operation"
    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: Publish volume but ID is too short to get first 24 bits
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I induce error "VolumeIDTooShortError"
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    Then the error contains "is shorter than 3 chars, returning error"
    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: Calling probe twice, so UpdateVolumePrefixToSystemsMap gets a key,value already added
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call Probe
    Then the error contains "none"

  Scenario Outline: Publish Volume with Wrong Access Types
    Given a VxFlexOS service
    And a valid volume
    And I use AccessType Mount
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    Then the error contains <msg>
    Examples:
      | access                | msg                                        | protocol  |
      | "multiple-writer"     | "mount multinode multi-writer not allowed" | "SDC"     |
      | "multi-single-writer" | "multinode single writer not supported"    | "SDC"     |
      | "multiple-writer"     | "mount multinode multi-writer not allowed" | "NVMeTCP" |
      | "multi-single-writer" | "multinode single writer not supported"    | "NVMeTCP" |

  Scenario: Idempotent publish volume with single writer
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    And I call PublishVolume with <access>
    Then a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 1

    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: Publish block volume with multiple writers to single writer volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    And then I use a different nodeID
    And I call PublishVolume with <access>
    Then the error contains "volume already published"
    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: Publish block volume with multiple writers to multiple writer volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "multiple-writer"
    And then I use a different nodeID
    And I call PublishVolume with "multiple-writer"
    Then a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 2
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish block volume with multiple writers to multiple reader volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "multiple-reader"
    And then I use a different nodeID
    And I call PublishVolume with "multiple-reader"
    Then the error contains "not compatible with access type"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish mount volume with multiple writers to single writer volume
    Given a VxFlexOS service
    And a valid volume
    And I use AccessType Mount
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with <access>
    And then I use a different nodeID
    And I call PublishVolume with <access>
    Then the error contains "volume already published"
    Examples:
      | access                      | protocol  |
      | "single-writer"             | "SDC"     |
      | "single-node-single-writer" | "SDC"     |
      | "single-node-multi-writer"  | "SDC"     |
      | "single-writer"             | "NVMeTCP" |
      | "single-node-single-writer" | "NVMeTCP" |
      | "single-node-multi-writer"  | "NVMeTCP" |

  Scenario: Publish mount volume with multiple readers to multiple reader volume
    Given a VxFlexOS service
    And a valid volume
    And I use AccessType Mount
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "multiple-reader"
    And then I use a different nodeID
    And I call PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish mount volume with multiple readers to multiple reader volume
    Given a VxFlexOS service
    And a valid volume
    And I use AccessType Mount
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "multiple-reader"
    And then I use a different nodeID
    And I call PublishVolume with "multiple-reader"
    Then a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 2
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with an invalid volumeID
    Given a VxFlexOS service
    When I call Probe
    And an invalid volume
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "volume not found"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume no volumeID specified
    Given a VxFlexOS service
    And no volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "volume ID is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with no nodeID specified
    Given a VxFlexOS service
    And a valid volume
    And no node
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "node ID is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with no volume capability
    Given a VxFlexOS service
    And a valid volume
    And no volume capability
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "volume capability is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with no access mode
    Given a VxFlexOS service
    And a valid volume
    And no access mode
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "access mode is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with getSDCID error
    Given a VxFlexOS service
    And a valid volume
    And I induce error "GetSdcInstancesError"
    When I call Probe
    And I call PublishVolume with "single-writer"
    Then the error contains "error getting host ID and type"

  Scenario: Publish volume with bad vol ID
    Given a VxFlexOS service
    And a valid volume
    And I induce error "BadVolIDError"
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains "volume not found"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Publish volume with a map SDC error
    Given a VxFlexOS service
    And a valid volume
    And I induce error <inducedErr>
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    Then the error contains <expectedErr>
    Examples:
      | protocol  | inducedErr     | expectedErr                         |
      | "SDC"     | "MapSdcError"  | "error mapping volume to node"      |
      | "NVMeTCP" | "MapNVMeError" | "error mapping volume to nvme host" |

  Scenario: Publish volume with AccessMode UNKNOWN
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "unknown"
    Then the error contains "access mode cannot be UNKNOWN"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |
 
  Scenario: Unpublish volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And the number of SDC mappings is 1 
    And I call UnpublishVolume
    And no error was received
    Then a valid UnpublishVolumeResponse is returned
    And the number of SDC mappings is 0
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |
     
  Scenario: Idempotent unpublish volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And I call UnpublishVolume
    And no error was received
    And I call UnpublishVolume
    And no error was received
    Then a valid UnpublishVolumeResponse is returned
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Unpublish volume with no volume id
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And no volume
    And I call UnpublishVolume
    Then the error contains "volume ID is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Unpublish volume with invalid volume id
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And an invalid volume
    And I call UnpublishVolume
    Then the error contains "volume not found"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Unpublish volume with no node id
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And no node
    And I call UnpublishVolume
    Then the error contains "Node ID is required"
    Examples:
      | protocol  |
      | "SDC"     |
      | "NVMeTCP" |

  Scenario: Unpublish volume with RemoveMappedSdcError
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "single-writer"
    And no error was received
    And I induce error <inducedErr>
    And I call UnpublishVolume
    Then the error contains <errorMsg>
    Examples:
      | protocol  | inducedErr              | errorMsg                                |
      | "SDC"     | "RemoveMappedSdcError"  | "Error unmapping volume from SDC node"  |
      | "NVMeTCP" | "RemoveMappedHostError" | "Error unmapping volume from NVMe host" |

  Scenario: Publish / unpublish mount volume with multiple writers to multiple writer volume
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I set protocol to <protocol>
    And I call PublishVolume with "multiple-writer"
    And a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 1
    And then I use a different nodeID
    And I call PublishVolume with "multiple-writer"
    And a valid PublishVolumeResponse is returned
    And the number of SDC mappings is 2
    And I call UnpublishVolume
    And no error was received
    And the number of SDC mappings is 1
    And then I use a different nodeID
    And I call UnpublishVolume
    And no error was received
    Then the number of SDC mappings is 0
    Examples:
      | protocol  |
      | "NVMeTCP" |

  Scenario: Create NFS volume, enable quota with all key parameters
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create NFS volume, enable quota without path key
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I specify NoPath
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "`path` is a required parameter"

  Scenario: Create NFS volume, enable quota without soft limit key
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I specify NoSoftLimit
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "`softLimit` is a required parameter"

  Scenario: Create NFS volume, enable quota without grace period key
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I specify NoGracePeriod
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "`gracePeriod` is a required parameter"

  Scenario: Create NFS volume, create quota error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And I induce error "CreateQuotaError"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "Creating quota failed"

  Scenario: Create NFS volume, enable quota, with FS quota disabled
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "86400"
    And a capability with voltype "mount" access "single-node-single-writer" fstype "nfs"
    And I induce error "ModifyFSError"
    When I call CreateVolumeSize nfs "vol-inttest-nfs" "8"
    Then the error contains "Modify filesystem failed"

  Scenario: Create NFS volume, empty path, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "" softLimit "20" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "path not set for volume"

  Scenario: Create NFS volume, empty soft limit, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "softLimit not set for volume"

  Scenario: Create NFS volume, empty graceperiod, set to default
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "80" graceperiod ""
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create NFS volume, invalid soft limit, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "abc" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "requested softLimit: abc is not numeric for volume"

  Scenario: Create NFS volume, invalid grace period, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "xyz"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "requested gracePeriod: xyz is not numeric for volume"

  Scenario: Create NFS volume, unlimited softlimit, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "0" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "requested softLimit: 0 perc, i.e. default value which is greater than hardlimit, i.e. volume size"

  Scenario: Create NFS volume, unlimited graceperiod
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "20" graceperiod "-1"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then a valid CreateVolumeResponse is returned

  Scenario: Create NFS volume, softlimit percentage greater than size, error
    Given a VxFlexOS service
    And I enable quota for filesystem
    And I set quota with path "/fs" softLimit "200" graceperiod "86400"
    And I call CreateVolumeSize nfs "vol-inttest-nfs" "10"
    Then the error contains "requested softLimit: 200 perc is greater than volume size"

  Scenario: Try to create NFS volume and Replication on NFS & Replication not supported Array
    Given a VxFlexOS service
    And I set Platform Info <sourceVersion> <sourceGenType> <targetVersion> <targetGenType>
    When I specify CreateVolumeMountRequest <volumeType>
    And I call CreateVolume "volume1" with Error <errorMsg>
    Then I reset the Platform Info

    Examples:
      | sourceVersion      | sourceGenType | targetVersion      | targetGenType | volumeType | errorMsg                            |
      | "5.0" | "EC" | "5.0" | "EC" | "nfs" | "NFS is not supported on the System"                              |
      | "4.0" | "" | "5.0" | "EC" | "ext4" | "Replication is not supported on this System" |
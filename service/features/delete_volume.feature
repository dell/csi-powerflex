Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test delete service methods
  So that they are known to work

  Scenario: Delete volume with valid CapacityRange capabilities BlockVolume, SINGLE_NODE_WRITER and null VolumeContentSource.
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call DeleteVolume with "single-writer"
    Then a valid DeleteVolumeResponse is returned

  Scenario: Delete volume with valid CapacityRange capabilities BlockVolume,  MULTI_NODE_READER_ONLY null VolumeContentSource.
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call DeleteVolume with "multiple-reader"
    Then a valid DeleteVolumeResponse is returned

  Scenario: Delete volume with valid CapacityRange capabilities BlockVolume, MULTI_NODE_WRITE null VolumeContentSource.
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call DeleteVolume with "multiple-writer"
    Then a valid DeleteVolumeResponse is returned

  Scenario: Test idempotent deletion volume valid CapacityRange capabilities BlockVolume, SINGLE_NODE_WRITER and null VolumeContentSource (2nd attempt to delete same volume should be nop.)
    Given a VxFlexOS service
    And a valid volume
    When I call Probe
    And I call DeleteVolume with "single-writer"
    And I call DeleteVolume with "single-writer"
    Then a valid DeleteVolumeResponse is returned
  
  Scenario: Test Basic nfs delete FileSystem
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call DeleteVolume nfs with "single-writer"
    Then a valid DeleteVolumeResponse is returned
  
   Scenario: a Basic Nfs delete FileSystem Bad
    Given a VxFlexOS service
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call NFS PublishVolume with "single-writer"
    Then a valid PublishVolumeResponse is returned
     And I call DeleteVolume nfs with "single-writer"
    Then the error contains "can not be deleted as it has associated NFS Export"
    
  Scenario: a Basic Nfs delete FileSystem with Snapshot
    Given a VxFlexOS service
    When I call Probe
    And I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call CreateSnapshot NFS "snap1"
    And no error was received
    And I call DeleteVolume nfs with "single-writer"
    Then the error contains "unable to delete NFS volume -- snapshots based on this volume still exist"

  Scenario: Test Idempotent Basic nfs delete FileSystem 
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I call DeleteVolume nfs with "single-writer"
    Then a valid DeleteVolumeResponse is returned
    And I call DeleteVolume nfs with "single-writer"
    Then a valid DeleteVolumeResponse is returned
    
    Scenario: Test Basic nfs delete FileSystem
    Given a VxFlexOS service
    When I call Probe
    When I specify CreateVolumeMountRequest "nfs"
    And I call CreateVolume "volume1"
    Then a valid CreateVolumeResponse is returned
    And I induce error "NFSExportsInstancesError"
    And I call DeleteVolume nfs with "single-writer"
    Then the error contains "error getting the NFS Export"

  Scenario: Delete volume with induced getVolByID error
    Given a VxFlexOS service
    And a valid volume
    And I induce error "GetVolByIDError"
    When I call Probe
    And I call DeleteVolume with "single-writer"
    Then the error contains "induced error"

  Scenario: Delete a volume with induced SIOGatewayVolumeNotFound error
    Given a VxFlexOS service
    And a valid volume
    And I induce error "SIOGatewayVolumeNotFound"
    When I call Probe
    And I call DeleteVolume with "single-writer"
    Then a valid DeleteVolumeResponse is returned

  Scenario: Delete volume with an invalid volume
    Given a VxFlexOS service
    And an invalid volume
    When I call Probe
    And I call DeleteVolume with "single-writer"
    Then the error contains "volume not found"

  Scenario: Delete volume with an invalid volume id
    Given a VxFlexOS service
    And a valid volume
    And I induce error "BadVolIDError"
    When I call Probe
    And I call DeleteVolume with "single-writer"
    Then a valid DeleteVolumeResponse is returned

  Scenario: Delete Volume negative
   Given a VxFlexOS service
   When I call Probe
   And I call DeleteVolume with Bad "single-writer"
   Then the error contains "volume ID is required"


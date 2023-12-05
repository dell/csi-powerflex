Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
  
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
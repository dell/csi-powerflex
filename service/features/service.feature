Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  @wip
  Scenario: Expand Nfs Volume
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "ext4"
    And a nfs volume request "nfsinttestvol2" "10"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "20"
    And there are no errors
    And I call ListVolume
    And a valid ListVolumeResponse is returned
    And when I call NodeUnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call UnpublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors
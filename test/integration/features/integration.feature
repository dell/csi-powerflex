Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Create block volume with access mode read write many
    Given a VxFlexOS service
    And a capability with voltype "block" access "multi-writer" fstype ""
    And a volume request "block-multi-writer-test" "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And when I call PublishVolume "ALT_GUID"
    And when I call NodePublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev1"
    And there are no errors
    And when I call NodePublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev2"
    And there are no errors
    And when I call NodePublishVolume "ALT_GUID"
    And there are no errors
    And when I call NodeUnpublishVolume "ALT_GUID"
    And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev1"
    And when I call NodeUnpublishVolumeWithPoint "SDC_GUID" "/tmp/tempdev2"
    And when I call UnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "ALT_GUID"
    And when I call DeleteVolume
    Then there are no errors
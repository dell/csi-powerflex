Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  Scenario: Create, publish, unpublish, and delete basic vol, change name of array and specify wrong allSystemNames , this will pass if volume because handle has id
    Given a VxFlexOS service
    And I set another systemID "altSystem"
    And Set System Name As "block-legacy-gateway"
    And Set Bad AllSystemNames
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And a volume request "integration9" "8"
    When I call CreateVolume
    And Set System Name As "1235e15806d1ec0f_pflex_system"
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
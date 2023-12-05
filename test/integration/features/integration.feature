Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
   
   Scenario Outline: Create, publish, unpublish, and delete basic vol, using systemName. Second run: sc has ID, but secret has name
    Given a VxFlexOS service
    And a capability with voltype "mount" access "single-writer" fstype "ext4"
    And I set another systemName "altSystem"
    And a volume request <name> "8"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume "SDC_GUID"
    And when I call NodeUnpublishVolume "SDC_GUID"
    And when I call UnpublishVolume "SDC_GUID"
    And there are no errors
    And when I call DeleteVolume
    Then there are no errors

    Examples:
      | name                         |
      | "integration7"               |
      | "alt_system_id_integration8" |
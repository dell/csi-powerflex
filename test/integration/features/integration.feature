Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.
   
   Scenario Outline: Scalability test to create volumes, publish, node publish, node unpublish, unpublish, delete volumes in parallel
    Given a VxFlexOS service
    And I set another systemID <id>
    When I create <numberOfVolumes> volumes in parallel
    And there are no errors
    And I publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node publish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I node unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And I unpublish <numberOfVolumes> volumes in parallel
    And there are no errors
    And when I delete <numberOfVolumes> volumes in parallel
    Then there are no errors
    Examples:
      | id              | numberOfVolumes |
      | "defaultSystem" | 5               |
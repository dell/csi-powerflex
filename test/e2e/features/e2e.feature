Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

  @zone
  Scenario: Create zone volume through k8s
    Given a VxFlexOS service
    And verify driver is configured and running correctly
    And verify zone information from secret <secret> in namespace <namespace>
    Then create zone volume and pod in <location>
    And check pods to be running on desired zones
    Then delete zone volume and pod in <location>
    Examples:
      | secret            | namespace  | location    |
      | "vxflexos-config" | "vxflexos" | "zone-wait" |

  @zone
  Scenario: Cordon node and create zone volume through k8s
    Given a VxFlexOS service
    And verify driver is configured and running correctly
    And verify zone information from secret <secret> in namespace <namespace>
    Then cordon one node
    Then create zone volume and pod in <location>
    And ensure pods aren't scheduled incorrectly and still running
    Then delete zone volume and pod in <location>
    Examples:
      | secret            | namespace  | location    |
      | "vxflexos-config" | "vxflexos" | "zone-wait" |


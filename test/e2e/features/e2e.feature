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
    And check the statefulset for zones
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
    And check the statefulset for zones
    And ensure pods aren't scheduled incorrectly and still running
    Then delete zone volume and pod in <location>
    Examples:
      | secret            | namespace  | location    |
      | "vxflexos-config" | "vxflexos" | "zone-wait" |

  @zone
  Scenario: Create zone voume and snapshots
    Given a VxFlexOS service
    And verify driver is configured and running correctly
    And verify zone information from secret <secret> in namespace <namespace>
    Then create zone volume and pod in <location>
    And check the statefulset for zones
    Then create snapshots for zone volumes and restore in <location>
    And all zone restores are running
    Then delete snapshots for zone volumes and restore in <location>
    Then delete zone volume and pod in <location>
    Examples:
      | secret            | namespace  | location    |
      | "vxflexos-config" | "vxflexos" | "zone-wait" |

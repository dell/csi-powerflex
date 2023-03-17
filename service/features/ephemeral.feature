 
#Note: there is some new Gherkin,  'controller published ephemeral volume'. 
#Why are we allowing the controller to publish an ephemeral volume when in practice, 
#an ephemeral volume would NEVER be published by controller before NodePublishVolume is called?
#It's needed because during unit tests, we don't actually write to an SDC, but ephemeralNodePublish expects us to.
#NodePublishVolume is expecting the volume returned by CreateVolume to be mapped to SDC, but since we're not using an SDC,
#it isn't. "a controller published ephemeral volume" is taking the volume ID given by CreateVolume, and mocking it's addition
#to the SDC. That way, when NodePublishVolume is called from ephemeralNodePublish, it can continue. 

Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test ephemeral volume  methods
  So that they are known to work

Scenario: Controller Publish Ephemeral Volume Fails
    Given a VxFlexOS service
    And a controller published ephemeral volume
    And a capability with voltype "mount" access "multi-writer" fstype "none"
    And get Node Publish Ephemeral Volume Request with name "csi-d0f055a700000000" size "30Gi" storagepool "viki_pool_HDD_20181031" and systemName "14dbbf5617523654"
    And I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains "inline ephemeral controller publish failed"

Scenario: Node Publish Ephemeral Volume Fails
    Given a VxFlexOS service
    And a controller published ephemeral volume
    And a capability with voltype "block" access "single-writer" fstype "none"
    And get Node Publish Ephemeral Volume Request with name "csi-d0f055a700000000" size "30Gi" storagepool "viki_pool_HDD_20181031" and systemName "14dbbf5617523654"
    And I induce error "NodePublishBlockTargetNotFile"
    When I call Probe
    When I call NodePublishVolume "SDC_GUID"
    Then the error contains "inline ephemeral node publish failed"

Scenario: Controller Unpublish Ephemeral Volume Fails 
    Given a VxFlexOS service
    And a controller published ephemeral volume
    And a capability with voltype "mount" access "single-reader" fstype "none"
    And I induce error "NoEndpointError" 
    And get Node Publish Ephemeral Volume Request with name "csi-d0f055a700000000" size "30Gi" storagepool "viki_pool_HDD_20181031" and systemName "14dbbf5617523654"
    And I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I induce error "BadVolIDError"
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains "Inline ephemeral controller unpublish failed"
 
Scenario Outline: Node publish and unpublish ephemeral volume
    Given a VxFlexOS service
    And a controller published ephemeral volume
    And a capability with voltype "mount" access "single-reader" fstype "none"
    And get Node Publish Ephemeral Volume Request with name <name> size <size> storagepool <storagepool> and systemName <systemName>
    And I call Probe
    And I call NodePublishVolume "SDC_GUID"
    And I call NodeUnpublishVolume "SDC_GUID"
    Then the error contains <errormsg>

Examples:
 |  name                   | size           | storagepool              | systemName         | errormsg                                |
 | "csi-d0f055a700000000"  | "30Gi"         | "viki_pool_HDD_20181031" | "14dbbf5617523654" | "none"                                  |
 | "csi-d0f055a700000000"  | "invalid size" | "viki_pool_HDD_20181031" | "14dbbf5617523654" | "inline ephemeral parse size failed"    |
 | "csi-d0f055a700000000"  | "30Gi"         | "viki_pool_HDD_20181031" | ""                 | "none"                                  |
 | "csi-d0f055a700000000"  | "30Gi"         | ""                       | "14dbbf5617523654" | "inline ephemeral create volume failed" |
 | ""                      | "30Gi"         | ""                       | "14dbbf5617523654" | "Volume name not specified"             |
 | "csi-thisnameisalittleover31characters"  | "30Gi"         | ""      | "14dbbf5617523654" | "Volume name too long"                  |
 | "csi-d0f055a700000000"  | "30Gi"         | "viki_pool_HDD_20181031" | "does-not-exist"   | "not recgonized"                        |
 | "csi-d0f055a700012345"  | "30Gi"         | "viki_pool_HDD_20181031" | "15dbbf5617523655-system-name" | "not published"             |

Scenario Outline: Ephemeral Node Unpublish with errors
	Given a VxFlexOS service
	And I induce error <error>
	And I call EphemeralNodeUnpublish 
	Then the error contains <errormsg>

Examples:
      | error             | errormsg                                        |
      | "NoVolumeIDError" | "volume ID is required"                         |
      | "none"            | "Inline ephemeral. Was unable to read lockfile" |

Scenario Outline: Ephemeral Node Publish with errors
        Given a VxFlexOS service
        And I call EphemeralNodePublish
        Then the error contains "not recgonized"

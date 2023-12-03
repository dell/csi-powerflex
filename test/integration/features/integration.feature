Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to run a system test
  So that I know the service functions correctly.

Scenario: Expand Nfs Volume with tree quota enabled given invalid volume size for exapnd volume
    Given a VxFlexOS service
    And a nfs capability with voltype "mount" access "single-writer" fstype "nfs"
    And a basic nfs volume request with quota enabled volname "vol-quota123" volsize "10" path "/nfs-quotakk" softlimit "80" graceperiod "86400"
    When I call CreateVolume
    And there are no errors
    And when I call PublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NodePublishVolume for nfs "SDC_GUID"
    And there are no errors
    And when I call NfsExpandVolume to "150000000"
    Then the error message should contain <errormsg>
    Examples:
    | errormsg    |
    | "NFS volume expansion failed with error: 400 Bad Request" |   
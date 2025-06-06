Feature: VxFlex OS CSI interface
  As a consumer of the CSI interface
  I want to test list service methods
  So that they are known to work

  Scenario: Test list volumes allowing an unlimited number of volumes
    Given a VxFlex OS service
    And there are 5 valid volumes
    When I call Probe
    And I call ListVolumes with max_entries "0" and starting_token "none"
    Then a valid ListVolumesResponse is returned
    And 5 volumes are listed

  Scenario: Test list volumes, limiting the number of volumes to be less than the number present using max_entries.
    Given a VxFlex OS service
    And there are 5 valid volumes
    When I call Probe
    And I call ListVolumes with max_entries "1" and starting_token "none"
    Then a valid ListVolumesResponse is returned
    And 1 volume is listed

  Scenario: Test list volumes starting at a different offset (using next_token)
    Given a VxFlex OS service
    And there are 5 valid volumes
    When I call Probe
    And I call ListVolumes with max_entries "2" and starting_token "none"
    And I call ListVolumes again with max_entries "3" and starting_token "next"
    Then a valid ListVolumesResponse is returned
    And 3 volumes are listed

  Scenario: Test list volumes with an invalid starting token
    Given a VxFlex OS service
    And a valid volume
    When I call Probe
    And I call ListVolumes with max_entries "1" and starting_token "invalid"
    Then an invalid ListVolumesResponse is returned

  Scenario: Test list volumes with induced volume instances error
    Given a VxFlex OS service
    And a valid volume
    And I induce error "VolumeInstancesError"
    When I call Probe
    And I call ListVolumes with max_entries "1" and starting_token "none"
    Then the error contains "Unable to list volumes"

  Scenario: Test list volumes with an starting token greater than volume count
    Given a VxFlex OS service
    And a valid volume
    When I call Probe
    And I call ListVolumes with max_entries "1" and starting_token "larger"
    Then an invalid ListVolumesResponse is returned

  Scenario Outline: Driver enforced ListVolumes pagination
    Given a VxFlex OS service
    And there are <volnum> valid volumes
    When I call Probe
    And I call ListVolumes with max_entries <volreq> and starting_token <starttok>
    Then a valid ListVolumesResponse is returned with <volres> entries and next_token <nexttok>

    Examples:
      | volnum    | volreq   | starttok   | volres    | nexttok |
      | 0         | "0"      | "0"        | "0"       | ""      |
      | 100       | "0"      | "0"        | "100"     | ""      |
      | 100       | "75"     | "0"        | "75"      | "75"    |
      | 100       | "75"     | "75"       | "25"      | ""      |
      | 100       | "300"    | "0"        | "100"     | ""      |
      | 205       | "0"      | "0"        | "100"     | "100"   |
      | 205       | "0"      | "100"      | "100"     | "200"   |
      | 205       | "0"      | "200"      | "5"       | ""      |


   Scenario: List snapshots
    Given a VxFlexOS service
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    And I call ListSnapshots with max_entries "5" and starting_token ""
    Then a valid ListSnapshotsResponse is returned with listed "5" and next_token ""

  Scenario: List snapshots with invalid starting token
    Given a VxFlexOS service
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    And I call ListSnapshots with max_entries "5" and starting_token "abcd"
    Then the error contains "Unable to parse StartingToken"

  Scenario: List snapshots with induced error reading snapshots
    Given a VxFlexOS service
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    And I induce error "VolumeInstancesError"
    And I call ListSnapshots with max_entries "5" and starting_token ""
    Then the error contains "Unable to list snapshots"

  Scenario: List snapshots with induced error badVolID
    Given a VxFlexOS service
    And there are 1 valid snapshots of "default" volume
    When I call Probe
    And I induce error "BadVolIDError"
    And I call ListSnapshots for volume "default"
    Then the error contains "none"


  Scenario: List snapshots two entries at times
    Given a VxFlexOS service
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    Then I call ListSnapshots with max_entries "2" and starting_token ""
    And a valid ListSnapshotsResponse is returned with listed "2" and next_token "2"
    And I call ListSnapshots with max_entries "2" and starting_token "2"
    And a valid ListSnapshotsResponse is returned with listed "2" and next_token "4"
    And I call ListSnapshots with max_entries "2" and starting_token "4"
    And a valid ListSnapshotsResponse is returned with listed "1" and next_token ""

  Scenario: List snapshots with 50000 entries
    Given a VxFlexOS service with timeout 120000 milliseconds
    And there are 50000 valid snapshots of "default" volume
    When I call Probe
    Then I call ListSnapshots with max_entries "9999" and starting_token ""
    And a valid ListSnapshotsResponse is returned with listed "9999" and next_token "9999"
    And I call ListSnapshots with max_entries "9999" and starting_token "9999"
    And a valid ListSnapshotsResponse is returned with listed "9999" and next_token "19998"
    And I call ListSnapshots with max_entries "9999" and starting_token "19998"
    And a valid ListSnapshotsResponse is returned with listed "9999" and next_token "29997"
    And I call ListSnapshots with max_entries "9999" and starting_token "29997"
    And a valid ListSnapshotsResponse is returned with listed "9999" and next_token "39996"
    And I call ListSnapshots with max_entries "9999" and starting_token "39996"
    And a valid ListSnapshotsResponse is returned with listed "9999" and next_token "49995"
    And I call ListSnapshots with max_entries "9999" and starting_token "49995"
    And a valid ListSnapshotsResponse is returned with listed "5" and next_token ""
    And the total snapshots listed is "50000"

  Scenario: List snapshots for a given volume ancestor
    Given a VxFlexOS service
    And a valid volume
    And there are 5 valid snapshots of "default" volume
    And there are 10 valid snapshots of "alt" volume
    When I call Probe
    Then I call ListSnapshots for volume "default"
    And a valid ListSnapshotsResponse is returned with listed "5" and next_token ""
    And I call ListSnapshots for volume "alt"
    And a valid ListSnapshotsResponse is returned with listed "10" and next_token ""

  Scenario: List a particular snapshot
    Given a VxFlexOS service
    And a valid volume
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    Then I call ListSnapshots for snapshot "14dbbf5617523654-3456"
    And a valid ListSnapshotsResponse is returned with listed "1" and next_token ""
    And the snapshot ID is "0000-3"

  Scenario: List a particular snapshot with induced error
    Given a VxFlexOS service
    And a valid volume
    And there are 5 valid snapshots of "default" volume
    When I call Probe
    And I induce error "GetVolByIDError"
    Then I call ListSnapshots for snapshot "14dbbf5617523654-3456"
    And the error contains "Unable to list volumes"

Feature: PowerFlex replication
  As a powerflex user, I want to test powerflex replication
  So that replication is known to work

@replication
Scenario Outline: Test GetReplicationCapabilities
  Given a VxFlexOS service
  And I use config "replication-config"
  And I induce error <error>
  When I call GetReplicationCapabilities
  Then the error contains <errormsg>
  And a <valid> replication capabilities structure is returned 
  Examples:
  | error  | errormsg | valid  |
  | "none" | "none"   | "true" | 
  
@replication
Scenario Outline: Test CreateRemoteVolume
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name>
  And I induce error <error>
  And I call CreateRemoteVolume
  Then the error contains <errormsg>
  Examples:
  | name        | error                        | errormsg                            |
  | "sourcevol" | "none"                       | "none"                              |
  | "sourcevol" | "NoVolIDError"               | "volume ID is required"             |
  | "sourcevol" | "controller-probe"           | "PodmonControllerProbeError"        |
  | "sourcevol" | "GetVolByIDError"            | "can't query volume"                |
  | "sourcevol" | "PeerMdmError"               | "PeerMdmError"                      |
  | "sourcevol" | "CreateVolumeError"          | "create volume induced error"       |
  | "sourcevol" | "BadVolIDError"              | "failed to provide"                 |
  | "sourcevol" | "BadRemoteSystemIDError"     | "System 15dbbf5617523655 not found" |
  | "sourcevol" | "ProbePrimaryError"          | "PodmonControllerProbeError"        |
  | "sourcevol" | "ProbeSecondaryError"        | "PodmonControllerProbeError"        |

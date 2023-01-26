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


@replication
Scenario Outline: Test CreateStorageProtectionGroup
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name>
  And I call CreateRemoteVolume
  And I induce error <error>
  And I call CreateStorageProtectionGroup
  Then the error contains <errormsg>

  Examples:
  | name        | error                                       | errormsg                                            |
  | "sourcevol" | "none"                                      | "none"                                              |
  | "sourcevol" | "NoVolIDError"                              | "volume ID is required"                             |
  | "sourcevol" | "BadVolIDError"                             | "failed to provide"                                 |
  | "sourcevol" | "EmptyParametersListError"                  | "empty parameters list"                             |
  | "sourcevol" | "controller-probe"                          | "PodmonControllerProbeError"                        |
  | "sourcevol" | "GetVolByIDError"                           | "can't query volume"                                |
  | "sourcevol" | "ReplicationConsistencyGroupError"          | "create rcg induced error"                          |
  | "sourcevol" | "GetReplicationConsistencyGroupsError"      | "could not GET ReplicationConsistencyGroups"        |
  | "sourcevol" | "GetRCGByIdError"                           | "could not GET RCG by ID"                           |
  | "sourcevol" | "ProbePrimaryError"                         | "PodmonControllerProbeError"                        |
  | "sourcevol" | "ProbeSecondaryError"                       | "PodmonControllerProbeError"                        |
  | "sourcevol" | "NoProtectionDomainError"                   | "NoProtectionDomainError"                           |
  | "sourcevol" | "ReplicationPairError"                      | "POST ReplicationPair induced error"                |
  | "sourcevol" | "PeerMdmError"                              | "PeerMdmError"                                      |
  | "sourcevol" | "BadRemoteSystem"                           | "couldn't getSystem (remote)"                       |
  | "sourcevol" | "FindVolumeIDError"                         | "can't find volume replicated-sourcevol by name"    |
  | "sourcevol" | "StorageGroupAlreadyExists"                 | "none"                                              |
  | "sourcevol" | "StorageGroupAlreadyExistsUnretriavable"    | "couldn't find replication consistency group"       |
  | "sourcevol" | "ReplicationPairAlreadyExists"              | "none"                                              |
  | "sourcevol" | "ReplicationPairAlreadyExistsUnretrievable" | "couldn't find replication pair"                    |
  | "sourcevol" | "NoRemoteSystem"                            | "no remote system specified"                        |
  | "sourcevol" | "NoRPOSpecified"                            | "no RPO specified"                                  |
  | "sourcevol" | "NoRemoteClusterID"                         | "no remote cluster ID specified"                    |

@replication
Scenario Outline: Test CreateStorageProtectionGroup with arguments
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name>
  And I call CreateRemoteVolume
  And I induce error <error>
  And I call CreateStorageProtectionGroup with <group name>, <remote cluster id>, <rpo>
  Then the error contains <errormsg>

  Examples:
  | name          | group name | remote cluster id | rpo  | error  | errormsg |
  | "sourcevol"   | "rcg-1"    | "cluster-k211"    | "60" | "none" | "none"   |
  | "sourcevol"   | ""         | "cluster-k211"    | "60" | "none" | "none"   |
  | "sourcevol"   | ""         | "self"            | "60" | "none" | "none"   |
  | "sourcevol"   | ""         | "k211-boston"     | "60" | "none" | "none"   |

@replication
Scenario Outline: Test multiple CreateStorageProtectionGroup calls
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name1>
  And I call CreateRemoteVolume
  And I call CreateStorageProtectionGroup with <group name>, <remote cluster id>, <rpo>
  When I call CreateVolume <name2>
  And I call CreateRemoteVolume
  And I call CreateStorageProtectionGroup with <group name>, <remote cluster id>, <rpo2>
  Then the error contains <errormsg>

  Examples:
  | name1     | name2     | group name | remote cluster id | rpo  | rpo2   | errormsg |
  | "1srcVol" | "2srcVol" | ""         | "cluster-k211"    | "60" | "60"   | "none"   |
  | "1srcVol" | "2srcVol" | ""         | "cluster-k211"    | "60" | "120"  | "none"   |

@replication
Scenario Outline: Test GetStorageProtectionGroupStatus 
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name>
  And I call CreateRemoteVolume
  And I call CreateStorageProtectionGroup
  And I induce error <error>
  And I call GetStorageProtectionGroupStatus
  Then the error contains <errormsg>

  Examples:
  | name        | error                     | errormsg                                           |
  | "sourcevol" | "none"                    | "none"                                             |
  | "sourcevol" | "GetRCGByIdError"         | "could not GET RCG by ID"                          |
  | "sourcevol" | "GetReplicationPairError" | "GET ReplicationPair induced error"                |

@replication
Scenario Outline: Test GetStorageProtectionGroupStatus current status
  Given a VxFlexOS service
  And I use config "replication-config"
  When I call CreateVolume <name>
  And I call CreateRemoteVolume
  And I call CreateStorageProtectionGroup
  And I call GetStorageProtectionGroupStatus with state <state> and mode <mode>
  Then the error contains <errormsg>

  Examples:
  | name        | errormsg   | state       | mode                  |
  | "sourcevol" | "none"     | "Normal"    | "Consistent"          |
  | "sourcevol" | "none"     | "Normal"    | "PartiallyConsistent" |
  | "sourcevol" | "none"     | "Normal"    | "ConsistentPending"   |
  | "sourcevol" | "none"     | "Normal"    | "Invalid"             |
  | "sourcevol" | "none"     | "Failover"  | "Consistent"          |
  | "sourcevol" | "none"     | "Paused"    | "Consistent"          |

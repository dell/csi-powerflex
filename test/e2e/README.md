# End-to-End Tests Overview

## Prerequisite

A couple of assumptions made in the e2e tests are that the driver runs with a valid secret. Other assumptions are:

1. The namespace `vxflexos-test` exists since all k8s objects will be created and checked for there.
2. For `multi-available-zone` tests, the storage class `vxflexos-az-wait` exists (see [storage class](../../samples/storageclass/storageclass-az.yaml) for example). 
3. For `multi-available-zone` tests, the nodes should be configured correctly as the test will pull out the zoneLabelKey and compare across the nodes and storage class.

## Using the Makefile

In the root `test` directory, there is a `Makefile` with different targets for the integration test and the e2e test. Currently, the only target is:

- `make zone-e2e` -> Runs the end to end tests for `multi-available-zone`.

## Overview of Targets/Tests

### Multi-Available Zone

The following tests are implemented and run during the `zone-e2e` test.

1. Creates a stateful set of 7 replicas and ensures that everything is up and ready with the zone configuration.
2. Cordons a node (marks it as unschedulable), creates 7 volumes/pods, and ensures that none gets scheduled on the cordoned node.

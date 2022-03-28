# [Optional] ginko options for custom runs
export GINKGO_OPTS="-v"

# [Optional] Path to .kube cnifguration if it is not in the deafult loacaltion
export KUBECONFIG="/root/.kube/config"

# Must suply path to values file
export E2E_VALUES_FILE="/root/test-e2e/csi-powerflex/test/e2e/testfiles/values.yaml"

# [Optional] namespace of operator if you deployed it to a namespace diffrent form the one below.
export DRIVER_NAMESPACE="vxflexos"

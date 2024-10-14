# FSGroup e2e Tests

## Installing Ginkgo v2

To install Ginkgo, use the following command:

```bash
go install github.com/onsi/ginkgo/v2/ginkgo
```

### Verify Installation

After installation, check the Ginkgo version with:

```bash
/root/go/bin/ginkgo version
```

You should see output similar to:

```
Ginkgo Version 2.x.x
```

Move the Ginkgo binary to a more accessible location:

```bash
mv /root/go/bin/ginkgo /usr/bin/ginkgo
```

## Running Tests

Install PowerFlex csi-driver and run the following command to execute FSGroup tests:

```bash
./run.sh
```

### Understanding the `--focus` Flag

The `--focus` flag filters the tests, looking for string matches in `Describe()` Ginkgo nodes. For example, in the file `fs.go`, you might see:

```go
ginkgo.Describe("Volume Filesystem Group Test", ginkgo.Label("csi-fsg"), ginkgo.Label("csi-fs"), ginkgo.Serial, func() {
    // Test implementation
})
```

## Test File Overview

### `fs.go`

This file creates a StorageClass, PersistentVolumeClaim (PVC) with that StorageClass, and a Pod that mounts this PVC, setting the `fsGroup` as specified.

- All necessary variables are declared in `e2e-values.yaml`.
- Helper methods to interact with Kubernetes are located in `utils.go`.
- Default timeouts for Kubernetes operations are defined in the E2E framework. CRUD operations are executed through:
  ```go
  fpod "k8s.io/kubernetes/test/e2e/framework/pod"
  fpv "k8s.io/kubernetes/test/e2e/framework/pv"
  fss "k8s.io/kubernetes/test/e2e/framework/statefulset"
  ```

### `fs_scaleup_scaledown.go`

This file includes more complex tests that utilize a StatefulSet to create a Pod and expose a PVC/PV. It scales pods and cordons a pod while using a YAML file to set up the StatefulSet, Pod, and PVC.

### Important Notes

- In case of errors, a timeout will activate, waiting for operations (e.g., 5 minutes for Pod creation, 10 minutes for StatefulSet).
- Cleanup occurs after each test upon success.
- Use `getEvents` for details and troubleshooting when needed.

## Test Structure

- The Ginkgo test suite is initialized in `suite_test.go` using `RunSpecs`.
- `ginkgo.Describe` defines a suite of scenarios, covering both happy paths and error conditions.
- `ginkgo.BeforeEach` specifies a setup method that runs before each test.
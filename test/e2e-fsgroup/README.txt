
# install ginkgo version 1
go get -u github.com/onsi/ginkgo/ginkgo

/root/go/bin/ginkgo version

Ginkgo Version 1.16.5

mv /root/go/bin/ginkgo /usr/bin/ginkgo


run.sh executes gingo  tests

# ginkgo -mod=mod --focus=FSGroup ./...

focus looks for string match in It() ginkgo node

#fs.go:  ginkgo.It("[csi-fsg] Verify Pod FSGroup", func() {


fs.go -- creates a StorageClass sc , PVC with this sc , Pod that mounts this PVC , and sets fsgroup as given

 - all variables are declared in e2e-values.yaml

 - utils.go has helper methods to call into k8s using e2e framework

 - default timeouts are defined in e2e framework , 
	k8s calls to do CRUD are defined here and called from utils.go

 	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
        fpv "k8s.io/kubernetes/test/e2e/framework/pv"
        fss "k8s.io/kubernetes/test/e2e/framework/statefulset"


#fs_scaleup_scaledown.go:        ginkgo.It("[csi-adv-fsg] Statefulset with Nodes Scale-up and Scale-down", func() {

 --more complex test using a statefulset to create a pod and expose a pvc/pv
	scale pods and cordon pod
	uses yaml file to setup statefulset / pod /pvc

notes :
	in case of errors , timeout kicks in and waits for example 5 minutes for pod create , 10 minutes for statefulset
	cleanup occurs in each test on success
	sometime getEvents is needed for details/troubleshooting





// ginkgo suite is kicked off in suite_test.go  RunSpecsWithDefaultAndCustomReporters

// ginkgo.Describe is a ginkgo spec , runs a set of scenarios , like one happy path and several error tests

// ginkgo.BeforeEach defines a method that will run once

// followed bg ginkgo.It() that defines one scenario

// below we run 3 scenarios ,
//      each one changes the fsGroupPolicy in csiDriver ,
//              restarts all driver pods ,
//                      creates a new pod similar to helm test , with pvc/pv which used the fsGroup value (non root )

//      each It method verifies expected results using gomega Matcher library

//      each It method deletes the pv/pvc/pod --cleanup

//      framework.Logf --will stop and not cleaup if we want to exit due to unexpected match , manual cleanup is expected

// notice the imports from framework, e2e test framework from kubernetes that provided methods to create/delete/list pv/pvc/pod

// utils.go calls these framework methods

// we can also create a pod or statefulset from a yaml file in

// testing-manifests folder. see GetStatefulSetFromManifest() in utils





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

 - all variables are declared as go  var 
	todo: put these in a yaml file , then fetch and use per ginkgo test

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



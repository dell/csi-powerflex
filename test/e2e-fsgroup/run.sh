export E2E_VALUES_FILE=testfiles/i.yaml
export E2E_VALUES_FILE=testfiles/d.yaml
export ACK_GINKGO_DEPRECATIONS=1.16.5
#go test -timeout=105s -v ./ -ginkgo.v=1

ginkgo -mod=mod ./...

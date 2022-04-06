
# supress ginkgo 2.0 upgrade hints
export ACK_GINKGO_DEPRECATIONS=1.16.5

# another way to run tests
#go test -timeout=105s -v ./ -ginkgo.v=1

# use focus to run only one test from fs_scaleup_scaledown.go
#ginkgo -mod=mod --focus=Scale ./...

# use focus to run only one test from fs.go
ginkgo -mod=mod --focus=FSGroup --timeout=25m ./...

# run ephemeral only test
ginkgo -mod=mod --focus=Ephemeral ./...

package service

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"testing"

	"github.com/cucumber/godog"
)

func TestMain(m *testing.M) {

	go http.ListenAndServe("localhost:6060", nil)
	fmt.Printf("starting godog...\n")

	status := m.Run()

	fmt.Printf("unit test status %d\n", status)

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
		//Tags:   "wip",
	}

	st := godog.TestSuite{
		Name:                "godog",
		ScenarioInitializer: FeatureContext,
		Options:             &opts,
	}.Run()

	if st > status {
		status = st
	}

	fmt.Printf("godog test status %d\n", status)

	os.Exit(status)
}

// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package service

import (
	"fmt"
	"sync"
	"testing"
)

func TestGetNVMeTargetNqnSnapshotReturnsCopy(t *testing.T) {
	svc := &service{
		nvmeTargetNqn: make(map[string]string),
	}

	svc.setNVMeTargetNqn("10.0.0.1", "nqn.original")

	snapshot := svc.getNVMeTargetNqnSnapshot()
	snapshot["10.0.0.1"] = "nqn.modified"
	snapshot["10.0.0.2"] = "nqn.new"

	live := svc.getNVMeTargetNqnSnapshot()
	if got := live["10.0.0.1"]; got != "nqn.original" {
		t.Fatalf("expected original value to remain unchanged, got %q", got)
	}
	if _, exists := live["10.0.0.2"]; exists {
		t.Fatalf("unexpected key from snapshot write leaked into live map")
	}
}

func TestSetAndSnapshotNVMeTargetNqnConcurrent(t *testing.T) {
	svc := &service{
		nvmeTargetNqn: make(map[string]string),
	}

	const (
		writers    = 24
		readers    = 24
		iterations = 1500
		keySpace   = 32
	)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var firstErr error
	var setErrOnce sync.Once
	recordErr := func(err error) {
		setErrOnce.Do(func() {
			firstErr = err
		})
	}

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				portal := fmt.Sprintf("10.0.0.%d", i%keySpace)
				nqn := fmt.Sprintf("nqn.writer.%d.iter.%d", writerID, i)
				svc.setNVMeTargetNqn(portal, nqn)
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				snapshot := svc.getNVMeTargetNqnSnapshot()
				for portal, nqn := range snapshot {
					if portal == "" {
						recordErr(fmt.Errorf("snapshot contains empty portal key"))
						return
					}
					if nqn == "" {
						recordErr(fmt.Errorf("snapshot contains empty NQN value for portal %s", portal))
						return
					}
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("concurrent snapshot validation failed: %v", firstErr)
	}

	finalSnapshot := svc.getNVMeTargetNqnSnapshot()
	if len(finalSnapshot) == 0 {
		t.Fatalf("expected final snapshot to contain discovered targets")
	}
}

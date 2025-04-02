/*
 Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMainFunction(t *testing.T) {
	// gitDescribeFunc gets current commit information.
	// unit tests should NEVER be dependent on variable environment
	// values, so override it with a known value for testing.
	defaultGitDescribeFunc := gitDescribeFunc

	afterEach := func() {
		gitDescribeFunc = defaultGitDescribeFunc
	}
	tests := []struct {
		name            string
		format          string
		outputFile      string
		setup           func()
		expectEmptyFile bool
		readFileFunc    func(file string) ([]byte, error)
	}{
		{
			name:       "Write mk format to file",
			format:     "mk",
			outputFile: "test_output.mk",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write env format to file",
			format:     "env",
			outputFile: "test_output.env",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write json format to file",
			format:     "json",
			outputFile: "test_output.json",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write ver format to file",
			format:     "ver",
			outputFile: "test_output.ver",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write rpm format to file",
			format:     "rpm",
			outputFile: "test_output.rpm",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write tpl format to file",
			format:     "../semver.tpl",
			outputFile: "test_output.rpm",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
		},
		{
			name:       "Write tpl format to file but error reading source file",
			format:     "../semver.tpl",
			outputFile: "test_output.rpm",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
			readFileFunc: func(_ string) ([]byte, error) {
				return nil, errors.New("error reading source file")
			},
			expectEmptyFile: true,
		},
		{
			// go format currently does not print any output, expect an empty file
			name:   "Write go format to file",
			format: "go",
			setup: func() {
				gitDescribeFunc = func() ([]byte, error) {
					return []byte("v2.14.0-notes-12-gd058795-dirty"), nil
				}
			},
			outputFile:      "test_output.go",
			expectEmptyFile: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			defer afterEach()

			osArgs := os.Args
			os.Args = append(os.Args, "-f", tt.format)
			os.Args = append(os.Args, "-o", tt.outputFile)
			os.Args = append(os.Args, "-x", "true")

			oldReadFile := ReadFile
			if tt.readFileFunc != nil {
				ReadFile = tt.readFileFunc
			}
			oldOSExit := OSExit
			OSExit = func(_ int) {}

			main()

			// Open the file
			file, err := os.Open(tt.outputFile)
			if err != nil {
				t.Error(err)
			}
			defer file.Close()

			// Read the file contents
			contents, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
			}

			defer os.Remove(tt.outputFile)

			// make sure file is not empty
			if tt.expectEmptyFile {
				assert.Equal(t, 0, len(contents))
			} else {
				assert.NotEqual(t, 0, len(contents))
			}
			os.Args = osArgs
			ReadFile = oldReadFile
			OSExit = oldOSExit
		})
	}
}

func TestChkErr(t *testing.T) {
	tests := []struct {
		name           string
		out            []byte
		err            error
		wantOut        string
		wantErr        bool
		getExitError   func(err error) (*exec.ExitError, bool)
		getStatusError func(exitError *exec.ExitError) (int, bool)
	}{
		{
			name:    "No error",
			out:     []byte("output"),
			err:     nil,
			wantOut: "output",
			wantErr: false,
			getExitError: func(_ error) (*exec.ExitError, bool) {
				return nil, true
			},
			getStatusError: func(_ *exec.ExitError) (int, bool) {
				return 0, true
			},
		},
		{
			name:    "Error with command",
			out:     []byte("output"),
			err:     errors.New("error"),
			wantOut: "",
			wantErr: true,
			getExitError: func(_ error) (*exec.ExitError, bool) {
				return nil, false
			},
			getStatusError: func(_ *exec.ExitError) (int, bool) {
				return 1, false
			},
		},
		{
			name:    "Error casting to ExitError",
			out:     []byte("output"),
			err:     errors.New("error"),
			wantOut: "",
			wantErr: true,
			getExitError: func(_ error) (*exec.ExitError, bool) {
				return nil, true
			},
			getStatusError: func(_ *exec.ExitError) (int, bool) {
				return 1, false
			},
		},
		{
			name:    "Error getting status from ExitError",
			out:     []byte("output"),
			err:     errors.New("error"),
			wantOut: "",
			wantErr: true,
			getExitError: func(_ error) (*exec.ExitError, bool) {
				return nil, false
			},
			getStatusError: func(_ *exec.ExitError) (int, bool) {
				return 0, true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			GetExitError = tt.getExitError
			GetStatusError = tt.getStatusError
			OSExit = func(_ int) {}

			gotOut := chkErr(tt.out, tt.err)
			if gotOut != tt.wantOut {
				t.Errorf("chkErr() gotOut = %v, want %v", gotOut, tt.wantOut)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     bool
	}{
		{
			name:     "File exists",
			filePath: "semver.go",
			want:     true,
		},
		{
			name:     "File does not exist",
			filePath: "non-existent.txt",
			want:     false,
		},
		{
			name:     "File path is empty",
			filePath: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fileExists(tt.filePath)
			if got != tt.want {
				t.Errorf("fileExists(%s) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestErrorExit(t *testing.T) {
	message := "error message"

	if os.Getenv("INVOKE_ERROR_EXIT") == "1" {
		errorExit(message)
		return
	}
	// call the test again with INVOKE_ERROR_EXIT=1 so the errorExit function is invoked and we can check the return code
	cmd := exec.Command(os.Args[0], "-test.run=TestErrorExit") // #nosec G204
	cmd.Env = append(os.Environ(), "INVOKE_ERROR_EXIT=1")

	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Println("Error creating stderr pipe:", err)
		return
	}

	if err := cmd.Start(); err != nil {
		t.Error(err)
	}

	buf := make([]byte, 1024)
	n, err := stderr.Read(buf)
	if err != nil {
		t.Error(err)
	}

	err = cmd.Wait()
	if e, ok := err.(*exec.ExitError); ok && e.Success() {
		t.Error(err)
	}

	// check the output is the message we logged in errorExit
	assert.Equal(t, message, string(buf[:n]))
}

// Mock for the gitDescribeFunc
type MockGitDescribe struct {
	mock.Mock
}

// Simulate the function call because of "dubious ownership" error
// when running in GH actions
func (m *MockGitDescribe) gitDescribeFunc() ([]byte, error) {
	args := m.Called()
	return args.Get(0).([]byte), args.Error(1)
}

func TestGitDescribe(t *testing.T) {
	mockGit := new(MockGitDescribe)

	expectedOutput := []byte("v1.0.0-0-gabcdef0-dirty")
	mockGit.On("gitDescribeFunc").Return(expectedOutput, nil)

	actual, err := mockGit.gitDescribeFunc()

	assert.Nil(t, err)
	assert.Equal(t, expectedOutput, actual)
	mockGit.AssertExpectations(t)
}

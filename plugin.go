// Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
//go:build linux && plugin
// +build linux,plugin

//go:generate go generate ./core

package main

import "C"

import (
	"github.com/dell/csi-vxflexos/v2/provider"
	"github.com/dell/csi-vxflexos/v2/service"
)

////////////////////////////////////////////////////////////////////////////////
//                              Go Plug-in                                    //
////////////////////////////////////////////////////////////////////////////////

// ServiceProviders is an exported symbol that provides a host program
// with a map of the service provider names and constructors.
var ServiceProviders = map[string]func() interface{}{
	service.Name: func() interface{} { return provider.New() },
}

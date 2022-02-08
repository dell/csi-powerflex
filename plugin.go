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

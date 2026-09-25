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

package provider

import (
	"context"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service"
	"github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/Ecosystems/container-storage-modules/src/gocsi"
	"google.golang.org/grpc"
)

// Log init
// var Log = logrus.New()
var log = csmlog.WithFields(csmlog.Fields{})

// New returns a new Mock Storage Plug-in Provider.
func New() gocsi.StoragePluginProvider {
	svc := service.New()

	// Create a wrapper that will return the interceptor after it's initialized
	interceptorWrapper := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		interceptor := service.GetOperationInterceptor()
		if interceptor != nil {
			return interceptor(ctx, req, info, handler)
		}
		// If interceptor not yet initialized, just call the handler
		return handler(ctx, req)
	}

	return &gocsi.StoragePlugin{
		Controller:                svc,
		Identity:                  svc,
		Node:                      svc,
		BeforeServe:               svc.BeforeServe,
		RegisterAdditionalServers: svc.RegisterAdditionalServers,
		Interceptors: []grpc.UnaryServerInterceptor{
			interceptorWrapper,
		},

		EnvVars: []string{
			// Enable request validation
			gocsi.EnvVarSpecReqValidation + "=true",

			// Enable serial volume access
			gocsi.EnvVarSerialVolAccess + "=true",
		},
	}
}

// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package metrics

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// latestServer is the most recently created SharedMetricsServer.
// GatewayMonitor.Start() will register its metrics here if set.
var (
	latestServer   *SharedMetricsServer
	latestServerMu sync.Mutex
)

// NewSharedMetricsServer creates a new SharedMetricsServer with an isolated
// prometheus.Registry and HTTP handler. Callers MUST call Start() or StartTLS()
// to begin serving metrics, and Stop() to gracefully shut down.
func NewSharedMetricsServer() *SharedMetricsServer {
	registry := prometheus.NewRegistry()
	mux := http.NewServeMux()
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	mux.Handle("/metrics", handler)
	s := &SharedMetricsServer{
		registry: registry,
		mux:      mux,
	}

	// Track the latest server so GatewayMonitor can register its metrics.
	latestServerMu.Lock()
	latestServer = s
	latestServerMu.Unlock()

	return s
}

// Register adds a prometheus collector to the server's registry.
func (s *SharedMetricsServer) Register(collector prometheus.Collector) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry.Register(collector)
}

// Start begins listening on port for HTTP metrics requests.
// port may be a bare number ("9090") or a colon-prefixed address (":9090");
// both formats are normalised by FormatMetricsAddr before binding.
func (s *SharedMetricsServer) Start(port string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ln, err := net.Listen("tcp", FormatMetricsAddr(port))
	if err != nil {
		return fmt.Errorf("starting metrics server on %s: %w", port, err)
	}

	s.addr = ln.Addr().String()
	s.server = &http.Server{Handler: s.mux}

	go func() {
		_ = s.server.Serve(ln)
	}()

	return nil
}

// StartTLS begins listening on port for HTTPS metrics requests using the certificate
// and private key files at certFile and keyFile. Both files must be provided; if
// either is empty, an error is returned. Use Start() for plain HTTP.
// port may be a bare number ("9090") or a colon-prefixed address (":9090");
// both formats are normalised by FormatMetricsAddr before binding.
func (s *SharedMetricsServer) StartTLS(port, certFile, keyFile string) error {
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("StartTLS requires both certFile and keyFile to be non-empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("loading TLS key pair for metrics server: %w", err)
	}

	ln, err := net.Listen("tcp", FormatMetricsAddr(port))
	if err != nil {
		return fmt.Errorf("starting TLS metrics server on %s: %w", port, err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	s.addr = ln.Addr().String()
	s.server = &http.Server{
		Handler:   s.mux,
		TLSConfig: tlsCfg,
	}

	go func() {
		tlsLn := tls.NewListener(ln, tlsCfg)
		_ = s.server.Serve(tlsLn)
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *SharedMetricsServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// GetAddr returns the address the server is listening on (for ephemeral :0 ports).
func (s *SharedMetricsServer) GetAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

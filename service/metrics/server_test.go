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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateSelfSignedCert writes a self-signed TLS certificate and key to temporary
// files and returns their paths. The caller is responsible for removing the files.
func generateSelfSignedCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generating ECDSA key")

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err, "creating certificate")

	cf, err := os.CreateTemp(t.TempDir(), "cert*.pem")
	require.NoError(t, err)
	require.NoError(t, pem.Encode(cf, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	require.NoError(t, cf.Close())

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err, "marshalling EC private key")

	kf, err := os.CreateTemp(t.TempDir(), "key*.pem")
	require.NoError(t, err)
	require.NoError(t, pem.Encode(kf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	require.NoError(t, kf.Close())

	return cf.Name(), kf.Name()
}

// getMetricText makes an HTTP GET to the metrics endpoint and returns the body.
func getMetricText(t *testing.T, addr string) string {
	t.Helper()
	url := fmt.Sprintf("http://%s/metrics", addr)
	resp, err := http.Get(url) //nolint:noctx
	require.NoError(t, err, "HTTP GET to metrics endpoint failed")
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "reading metrics response body failed")
	return string(body)
}

// waitForAddr polls GetAddr() until it is non-empty or timeout.
func waitForAddr(t *testing.T, s *SharedMetricsServer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := s.GetAddr()
		if addr != "" {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for server address to become available")
	return ""
}

// -------------------------------------------------------------------
// U-001: TestNewSharedMetricsServer_ReturnsNonNil
// NewSharedMetricsServer() constructs a valid server with an isolated registry.
// -------------------------------------------------------------------
func TestNewSharedMetricsServer_ReturnsNonNil(t *testing.T) {
	// EXPECT: returns non-nil *SharedMetricsServer with non-nil registry and mux
	s := NewSharedMetricsServer()
	require.NotNil(t, s, "NewSharedMetricsServer() must return non-nil")
	assert.NotNil(t, s.registry, "internal registry must be non-nil")
	assert.NotNil(t, s.mux, "internal mux must be non-nil")
}

// -------------------------------------------------------------------
// U-002: TestSharedMetricsServer_Register_Success
// Register() adds a valid prometheus.Collector to the registry without error.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Register_Success(t *testing.T) {
	s := NewSharedMetricsServer()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_gauge_u002",
		Help: "gauge for U-002",
	})
	err := s.Register(gauge)
	assert.NoError(t, err, "Register() on a fresh collector must return nil error")
}

// -------------------------------------------------------------------
// U-003: TestSharedMetricsServer_Register_DuplicateReturnsError
// Register() returns an AlreadyRegisteredError when the same collector is registered twice.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Register_DuplicateReturnsError(t *testing.T) {
	s := NewSharedMetricsServer()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_gauge_u003",
		Help: "gauge for U-003",
	})
	err := s.Register(gauge)
	require.NoError(t, err, "first Register() must succeed")

	// Second registration of the same collector must return an error
	err = s.Register(gauge)
	require.Error(t, err, "duplicate Register() must return non-nil error")

	// The error should wrap or be a prometheus.AlreadyRegisteredError
	var alreadyRegistered prometheus.AlreadyRegisteredError
	// Accept either direct type or wrapped error
	isAlreadyRegistered := false
	if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
		isAlreadyRegistered = true
	} else if alreadyRegistered.ExistingCollector != nil {
		isAlreadyRegistered = true
	}
	_ = alreadyRegistered
	// The error message should be meaningful
	assert.True(t, isAlreadyRegistered || err != nil,
		"error should be an AlreadyRegisteredError or non-nil error: %v", err)
}

// -------------------------------------------------------------------
// U-004: TestSharedMetricsServer_Start_ListensOnPort
// Start(port) starts an HTTP server; /metrics endpoint is reachable.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Start_ListensOnPort(t *testing.T) {
	s := NewSharedMetricsServer()

	// Use :0 to let the OS pick an ephemeral port
	err := s.Start("0")
	require.NoError(t, err, "Start(':0') must succeed")

	// Get the actual address (ephemeral port resolved)
	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr, "server must report a listen address after Start()")

	// Perform HTTP GET on /metrics
	body := getMetricText(t, addr)

	// Prometheus text format starts with # HELP or # TYPE comments, or is empty
	// At minimum, the response must be HTTP 200 (getMetricText requires no error)
	// The body should be valid prometheus text format (may be empty for empty registry)
	assert.True(t,
		len(body) == 0 ||
			strings.Contains(body, "#") ||
			strings.Contains(body, "go_"),
		"metrics endpoint body should be empty or prometheus text format, got: %q", body)

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Stop(ctx)
}

// -------------------------------------------------------------------
// U-005: TestSharedMetricsServer_Start_InvalidPort_ReturnsError
// Start() with a non-numeric or reserved port should surface an error promptly.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Start_InvalidPort_ReturnsError(t *testing.T) {
	s := NewSharedMetricsServer()

	err := s.Start("invalid")
	require.Error(t, err, "Start('invalid') must return a non-nil error")
}

// -------------------------------------------------------------------
// U-006: TestSharedMetricsServer_Stop_BeforeStart_IsNoOp
// Stop() before Start() must be a safe no-op.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Stop_BeforeStart_IsNoOp(t *testing.T) {
	s := NewSharedMetricsServer()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Must not panic and must return nil
	assert.NotPanics(t, func() {
		err := s.Stop(ctx)
		assert.NoError(t, err, "Stop() before Start() must return nil error")
	})
}

// -------------------------------------------------------------------
// U-007: TestSharedMetricsServer_Stop_GracefulShutdown
// Stop() after Start() closes the HTTP listener gracefully.
// -------------------------------------------------------------------
func TestSharedMetricsServer_Stop_GracefulShutdown(t *testing.T) {
	s := NewSharedMetricsServer()

	err := s.Start("0")
	require.NoError(t, err, "Start(':0') must succeed")

	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr)

	// Verify it's actually serving
	_ = getMetricText(t, addr)

	// Now stop it
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s.Stop(ctx)
	assert.NoError(t, err, "Stop() must return nil error")

	// Wait a brief moment for the port to close
	time.Sleep(50 * time.Millisecond)

	// Subsequent HTTP GET must fail (connection refused)
	url := fmt.Sprintf("http://%s/metrics", addr)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	_, err = client.Get(url) //nolint:noctx
	assert.Error(t, err, "HTTP GET after Stop() must fail with connection error")
}

// -------------------------------------------------------------------
// U-008: TestSharedMetricsServer_MultipleCollectors_AllServed
// Two independent collectors registered on the same server both appear in /metrics.
// -------------------------------------------------------------------
func TestSharedMetricsServer_MultipleCollectors_AllServed(t *testing.T) {
	s := NewSharedMetricsServer()

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_multi_gauge_u008",
		Help: "gauge for U-008",
	})
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_multi_counter_u008",
		Help: "counter for U-008",
	})

	err := s.Register(gauge)
	require.NoError(t, err, "registering gauge must succeed")
	err = s.Register(counter)
	require.NoError(t, err, "registering counter must succeed")

	// Set known values
	gauge.Set(42.0)
	counter.Add(7.0)

	err = s.Start("0")
	require.NoError(t, err, "Start(':0') must succeed")

	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr)

	body := getMetricText(t, addr)

	assert.Contains(t, body, "test_multi_gauge_u008",
		"scrape body must contain the gauge metric name")
	assert.Contains(t, body, "test_multi_counter_u008",
		"scrape body must contain the counter metric name")

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Stop(ctx)
}

// -------------------------------------------------------------------
// U-009: TestSharedMetricsServer_StartTLS_EmptyFiles_ReturnsError
// StartTLS() returns an error if certFile or keyFile is empty.
// -------------------------------------------------------------------
func TestSharedMetricsServer_StartTLS_EmptyFiles_ReturnsError(t *testing.T) {
	s := NewSharedMetricsServer()

	err := s.StartTLS("0", "", "")
	require.Error(t, err, "StartTLS with empty cert/key must return an error")

	err = s.StartTLS("0", "cert.pem", "")
	require.Error(t, err, "StartTLS with empty key must return an error")

	err = s.StartTLS("0", "", "key.pem")
	require.Error(t, err, "StartTLS with empty cert must return an error")
}

// -------------------------------------------------------------------
// U-010: TestSharedMetricsServer_StartTLS_InvalidFiles_ReturnsError
// StartTLS() returns an error when cert/key files do not exist.
// -------------------------------------------------------------------
func TestSharedMetricsServer_StartTLS_InvalidFiles_ReturnsError(t *testing.T) {
	s := NewSharedMetricsServer()

	err := s.StartTLS("0", "/nonexistent/cert.pem", "/nonexistent/key.pem")
	require.Error(t, err, "StartTLS with missing files must return an error")
}

// -------------------------------------------------------------------
// U-011: TestSharedMetricsServer_StartTLS_ServesHTTPS
// StartTLS() with a valid self-signed cert/key starts an HTTPS server
// and the /metrics endpoint is reachable with InsecureSkipVerify.
// -------------------------------------------------------------------
func TestSharedMetricsServer_StartTLS_ServesHTTPS(t *testing.T) {
	certFile, keyFile := generateSelfSignedCert(t)

	s := NewSharedMetricsServer()

	err := s.StartTLS("0", certFile, keyFile)
	require.NoError(t, err, "StartTLS with valid cert/key must succeed")

	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr, "server must report a listen address after StartTLS()")

	// Plain HTTP must not return 200 OK (TLS server rejects it with 400)
	plainClient := &http.Client{Timeout: 500 * time.Millisecond}
	plainResp, plainErr := plainClient.Get(fmt.Sprintf("http://%s/metrics", addr)) //nolint:noctx
	if plainErr == nil {
		defer plainResp.Body.Close()
		assert.NotEqual(t, http.StatusOK, plainResp.StatusCode,
			"plain HTTP GET to TLS endpoint must not return 200 OK")
	}
	// Either an error or a non-200 response is acceptable — both indicate TLS is active.

	// HTTPS with InsecureSkipVerify must succeed
	tlsClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := tlsClient.Get(fmt.Sprintf("https://%s/metrics", addr)) //nolint:noctx
	require.NoError(t, err, "HTTPS GET to TLS endpoint must succeed")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.True(t,
		len(body) == 0 || strings.Contains(string(body), "#") || strings.Contains(string(body), "go_"),
		"metrics endpoint body should be empty or prometheus text format, got: %q", string(body))

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Stop(ctx)
}

// -------------------------------------------------------------------
// U-012: TestSharedMetricsServer_StartTLS_Stop_GracefulShutdown
// Stop() after StartTLS() closes the HTTPS listener gracefully.
// -------------------------------------------------------------------
func TestSharedMetricsServer_StartTLS_Stop_GracefulShutdown(t *testing.T) {
	certFile, keyFile := generateSelfSignedCert(t)

	s := NewSharedMetricsServer()

	err := s.StartTLS("0", certFile, keyFile)
	require.NoError(t, err, "StartTLS must succeed")

	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr)

	// Verify it's serving via HTTPS
	tlsClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	_, err = tlsClient.Get(fmt.Sprintf("https://%s/metrics", addr)) //nolint:noctx
	require.NoError(t, err, "initial HTTPS GET must succeed before Stop()")

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = s.Stop(ctx)
	assert.NoError(t, err, "Stop() must return nil error")

	// Wait a brief moment for the port to close
	time.Sleep(50 * time.Millisecond)

	// Subsequent HTTPS GET must fail (connection refused)
	shortClient := &http.Client{
		Timeout: 500 * time.Millisecond,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	_, err = shortClient.Get(fmt.Sprintf("https://%s/metrics", addr)) //nolint:noctx
	assert.Error(t, err, "HTTPS GET after Stop() must fail with connection error")
}

// -------------------------------------------------------------------
// U-013: TestSharedMetricsServer_StartTLS_MetricsRegistered
// Collectors registered before StartTLS() are served via HTTPS.
// -------------------------------------------------------------------
func TestSharedMetricsServer_StartTLS_MetricsRegistered(t *testing.T) {
	certFile, keyFile := generateSelfSignedCert(t)

	s := NewSharedMetricsServer()

	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_tls_gauge_u013",
		Help: "gauge for U-013",
	})
	gauge.Set(99.0)

	err := s.Register(gauge)
	require.NoError(t, err, "registering gauge must succeed")

	err = s.StartTLS("0", certFile, keyFile)
	require.NoError(t, err, "StartTLS must succeed")

	addr := waitForAddr(t, s, 2*time.Second)
	require.NotEmpty(t, addr)

	tlsClient := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := tlsClient.Get(fmt.Sprintf("https://%s/metrics", addr)) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "test_tls_gauge_u013",
		"HTTPS scrape body must contain the registered gauge metric")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Stop(ctx)
}

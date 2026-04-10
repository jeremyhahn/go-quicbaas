// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package backup

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// Backend availability flags set during TestMain
var (
	clusterReady     bool
	localStackReady  bool
	minioReady       bool
	azuriteReady     bool
	gcsEmulatorReady bool
)

// Environment variable names for backend endpoints
const (
	envQuicBaaSNode1URL     = "QUICBAAS_NODE1_URL"
	envS3Endpoint           = "S3_ENDPOINT"
	envMinIOEndpoint        = "MINIO_ENDPOINT"
	envAzureStorageEndpoint = "AZURE_STORAGE_ENDPOINT"
	envGCSEndpoint          = "GCS_ENDPOINT"
)

// Default endpoints for emulators.
// These use Docker service hostnames for the integration test container.
const (
	defaultQuicBaaSEndpoint = "https://test-node1:8080"
	defaultS3Endpoint       = "http://localstack:4566"
	defaultMinIOEndpoint    = "minio:9000"
	defaultAzuriteEndpoint  = "http://azurite:10000"
	defaultGCSEndpoint      = "http://fake-gcs-server:4443"
)

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	// Check cluster readiness
	clusterReady = waitForClusterReady()
	if !clusterReady {
		log.Fatal("QuicBaaS cluster must be ready for backup integration tests")
	}

	// Check LocalStack (S3 emulator) availability
	localStackReady = checkS3Availability()
	if localStackReady {
		log.Println("LocalStack (S3) is available for backup tests")
	} else {
		log.Fatal("LocalStack (S3) must be available for backup integration tests")
	}

	// Check MinIO availability
	minioReady = checkMinIOAvailability()
	if minioReady {
		log.Println("MinIO is available for backup tests")
	} else {
		log.Fatal("MinIO must be available for backup integration tests")
	}

	// Check Azurite (Azure emulator) availability
	azuriteReady = checkAzuriteAvailability()
	if azuriteReady {
		log.Println("Azurite (Azure) is available for backup tests")
	} else {
		log.Fatal("Azurite (Azure) must be available for backup integration tests")
	}

	// Check GCS emulator availability
	gcsEmulatorReady = checkGCSAvailability()
	if gcsEmulatorReady {
		log.Println("GCS emulator is available for backup tests")
	} else {
		log.Fatal("GCS emulator must be available for backup integration tests")
	}

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
func waitForClusterReady() bool {
	endpoint := getQuicBaaSEndpoint()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/api/v1/health/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Println("QuicBaaS cluster is ready for backup tests")
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// checkS3Availability checks if LocalStack (S3 emulator) is available.
func checkS3Availability() bool {
	endpoint := getS3Endpoint()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// LocalStack health check endpoint
	resp, err := client.Get(endpoint + "/_localstack/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// checkMinIOAvailability checks if MinIO is available.
func checkMinIOAvailability() bool {
	endpoint := getMinIOEndpoint()

	// Try to connect to MinIO endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}

// checkAzuriteAvailability checks if Azurite (Azure emulator) is available.
func checkAzuriteAvailability() bool {
	endpoint := getAzuriteEndpoint()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// Azurite responds to root path
	resp, err := client.Get(endpoint)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Azurite returns various status codes; just check if we got a response
	return resp.StatusCode < 500
}

// checkGCSAvailability checks if GCS emulator is available.
func checkGCSAvailability() bool {
	endpoint := getGCSEndpoint()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}

	// GCS emulator storage endpoint
	resp, err := client.Get(endpoint + "/storage/v1/b")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode < 500
}

// getQuicBaaSEndpoint returns the QuicBaaS REST API endpoint.
func getQuicBaaSEndpoint() string {
	if endpoint := os.Getenv(envQuicBaaSNode1URL); endpoint != "" {
		return endpoint
	}
	return defaultQuicBaaSEndpoint
}

// getS3Endpoint returns the S3/LocalStack endpoint.
func getS3Endpoint() string {
	if endpoint := os.Getenv(envS3Endpoint); endpoint != "" {
		return endpoint
	}
	// Also check AWS_ENDPOINT_URL which docker-compose may set
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		return endpoint
	}
	return defaultS3Endpoint
}

// getMinIOEndpoint returns the MinIO endpoint.
func getMinIOEndpoint() string {
	if endpoint := os.Getenv(envMinIOEndpoint); endpoint != "" {
		return endpoint
	}
	return defaultMinIOEndpoint
}

// getAzuriteEndpoint returns the Azurite endpoint.
func getAzuriteEndpoint() string {
	if endpoint := os.Getenv(envAzureStorageEndpoint); endpoint != "" {
		return endpoint
	}
	// Also check AZURITE_ENDPOINT which docker-compose may set
	if endpoint := os.Getenv("AZURITE_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return defaultAzuriteEndpoint
}

// getGCSEndpoint returns the GCS emulator endpoint.
func getGCSEndpoint() string {
	if endpoint := os.Getenv(envGCSEndpoint); endpoint != "" {
		return endpoint
	}
	// Also check STORAGE_EMULATOR_HOST which docker-compose may set
	if endpoint := os.Getenv("STORAGE_EMULATOR_HOST"); endpoint != "" {
		return endpoint
	}
	return defaultGCSEndpoint
}

// requireCluster fails the test if the cluster is not ready.
func requireCluster(t *testing.T) {
	t.Helper()
	if !clusterReady {
		t.Fatal("QuicBaaS cluster must be available for this test")
	}
}

// requireLocalStack fails the test if LocalStack is not available.
func requireLocalStack(t *testing.T) {
	t.Helper()
	if !localStackReady {
		t.Fatal("LocalStack (S3) must be available for this test")
	}
}

// requireMinIO fails the test if MinIO is not available.
func requireMinIO(t *testing.T) {
	t.Helper()
	if !minioReady {
		t.Fatal("MinIO must be available for this test")
	}
}

// requireAzurite fails the test if Azurite is not available.
func requireAzurite(t *testing.T) {
	t.Helper()
	if !azuriteReady {
		t.Fatal("Azurite (Azure) must be available for this test")
	}
}

// requireGCSEmulator fails the test if GCS emulator is not available.
func requireGCSEmulator(t *testing.T) {
	t.Helper()
	if !gcsEmulatorReady {
		t.Fatal("GCS emulator must be available for this test")
	}
}

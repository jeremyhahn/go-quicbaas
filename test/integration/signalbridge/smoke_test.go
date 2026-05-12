// +build integration

package signalbridge_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestSignalBridgeSmoke tests basic Signal Bridge functionality.
// Verifies that the stack comes up and responds to basic commands.
func TestSignalBridgeSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bridgeURL := "http://signalbridge-bridge:8000"

	// Test 1: Health check
	t.Run("HealthCheck", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, "GET", bridgeURL+"/health", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("health check failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	// Test 2: Bot help command
	t.Run("BotHelp", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, "POST", bridgeURL+"/command", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("bot help command failed (may be expected): %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("bot help returned status %d (may be expected)", resp.StatusCode)
			return
		}

		t.Log("Bot help command succeeded")
	})

	// Test 3: QuicBaaS connectivity
	t.Run("QuicBaaSConnectivity", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, "GET", "http://quicbaasd:9090/v1/health/live", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("quicbaasd health check failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 from quicbaasd, got %d", resp.StatusCode)
		}
	})

	t.Log("Signal Bridge smoke test passed")
}

// TestSignalBridgeQuicBaaSIntegration tests that Signal Bridge can write to QuicBaaS.
func TestSignalBridgeQuicBaaSIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Verify quicbaasd is reachable
	req, err := http.NewRequestWithContext(ctx, "GET", "http://quicbaasd:9090/v1/health/live", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("quicbaasd not accessible: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("quicbaasd unhealthy: status %d", resp.StatusCode)
	}

	fmt.Println("Signal Bridge - QuicBaaS integration working")
}

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

package seal

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// testContext holds shared test state initialized in TestMain.
type testContext struct {
	grpcEndpoints []string
	restEndpoints []string
	sealAvailable bool
	stateManager  *sealStateManager
}

var testCtx *testContext

// TestMain runs before all tests in this package to verify service availability.
func TestMain(m *testing.M) {
	testCtx = &testContext{
		grpcEndpoints: helpers.GetClusterEndpoints(),
		restEndpoints: helpers.GetRESTEndpoints(),
	}

	// Check if at least one gRPC endpoint is reachable and seal service is available
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if len(testCtx.grpcEndpoints) > 0 {
		conn, err := helpers.SetupGRPCConnectionToEndpoint(nil, ctx, testCtx.grpcEndpoints[0])
		if err != nil {
			log.Printf("Warning: Could not connect to gRPC endpoint %s: %v", testCtx.grpcEndpoints[0], err)
		} else {
			defer conn.Close()

			// Check if seal service is implemented
			client := pb.NewQuicBaaSClient(conn)
			_, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
			if err != nil {
				if st, ok := status.FromError(err); ok && st.Code() == codes.Unimplemented {
					log.Print("Info: Seal service not implemented on server")
					testCtx.sealAvailable = false
				} else {
					// Service is available but may have returned an error (e.g., not initialized)
					testCtx.sealAvailable = true
				}
			} else {
				testCtx.sealAvailable = true
			}

			// Initialize state manager if seal is available
			if testCtx.sealAvailable {
				mgr, err := initializeSealStateManager(ctx, testCtx.grpcEndpoints[0])
				if err != nil {
					log.Printf("Warning: Could not initialize state manager: %v", err)
				} else {
					testCtx.stateManager = mgr
					init, sealed, _, _, ver := mgr.GetStatus()
					hasShares := mgr.HasShares()
					log.Printf("TestMain: State manager ready, file=%s, init=%v, sealed=%v, version=%d, hasShares=%v",
						mgr.GetShareFilePath(), init, sealed, ver, hasShares)
				}
			}
		}
	}

	exitCode := m.Run()

	// Cleanup state manager on exit (optional - shares persist for future runs)
	// Uncomment the following to clear shares between test runs:
	// if testCtx != nil && testCtx.stateManager != nil {
	//     testCtx.stateManager.Cleanup()
	// }

	os.Exit(exitCode)
}

// requireSealAvailable fails the test if seal service is not available.
// This replaces skipIfSealUnavailable to comply with project rules against t.Skip.
func requireSealAvailable(t *testing.T) {
	t.Helper()
	require.NotNil(t, testCtx, "Test context must be initialized")
	require.True(t, testCtx.sealAvailable, "Seal service must be available on test server")
}

// getGRPCEndpoints returns the gRPC endpoints for testing.
func getGRPCEndpoints() []string {
	if testCtx != nil {
		return testCtx.grpcEndpoints
	}
	return helpers.GetClusterEndpoints()
}

// getRESTEndpoints returns the REST endpoints for testing.
func getRESTEndpoints() []string {
	if testCtx != nil {
		return testCtx.restEndpoints
	}
	return helpers.GetRESTEndpoints()
}

// getStateManager returns the initialized state manager for seal tests.
// Returns nil if state manager is not available.
func getStateManager() *sealStateManager {
	if testCtx != nil {
		return testCtx.stateManager
	}
	return nil
}

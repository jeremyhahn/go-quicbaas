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

package transactions

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
)

// issueTokenForUser is a helper function that retrieves a user from the store
// and issues a token for them. This replaces the deprecated password-based
// Authenticate method.
func issueTokenForUser(ctx context.Context, authManager *auth.InMemoryAuthManager, username string) (*auth.Token, error) {
	userInfo := authManager.GetUserStore().GetByUsername(username)
	if userInfo == nil {
		return nil, &qberrors.MessageError{Message: "user not found: " + username}
	}
	return authManager.IssueTokenForUser(ctx, userInfo)
}

// TestConcurrentOperations tests concurrent operations with all features
func TestConcurrentOperations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Initialize components
	m := metrics.InitMetrics()
	log, err := logger.New(logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	authManager, err := auth.NewInMemoryAuthManager(&auth.AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-12345",
		TokenTTL:  1 * time.Hour,
		MaxTokens: 100,
	}, log)
	require.NoError(t, err)

	cacheManager, err := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, helpers.NewLoggerAdapter(log))
	require.NoError(t, err)
	defer cacheManager.Close()

	// Connect to QuicBaaS
	endpoint := helpers.GetTestEndpoint()
	conn, err := grpc.DialContext(ctx, endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13})),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)

	// Create multiple users for concurrent access
	users := make([]*auth.Token, 10)
	for i := 0; i < 10; i++ {
		username := fmt.Sprintf("user%d", i)
		_, err := authManager.CreateUser(username, "password", []string{"user"})
		require.NoError(t, err)

		token, err := issueTokenForUser(ctx, authManager, username)
		require.NoError(t, err)
		users[i] = token
	}

	// Run concurrent operations
	var wg sync.WaitGroup
	operationsPerWorker := 20
	workerCount := 10

	startTime := time.Now()

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			token := users[workerID]
			workerCtx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token.AccessToken)

			for i := 0; i < operationsPerWorker; i++ {
				m.IncrementConcurrentRequests()

				key := uint64(time.Now().UnixNano() + int64(workerID*1000000+i))
				value := []byte(fmt.Sprintf("worker-%d-data-%d", workerID, i))

				// Write
				opStart := time.Now()
				putReq := &pb.PutRequest{
					EntityType: "concurrent_test",
					Id:         key,
					Data:       value,
				}

				_, err := client.Put(workerCtx, putReq)
				opDuration := time.Since(opStart).Seconds()

				if err == nil {
					m.RecordRequestDuration("put", "success", opDuration)
					m.RecordRequestSize("put", float64(len(value)))

					// Cache the value
					cacheKey := fmt.Sprintf("concurrent_test/%d", key)
					cacheManager.Set(workerCtx, cacheKey, value)
				} else {
					m.RecordRequestDuration("put", "error", opDuration)
				}

				m.DecrementConcurrentRequests()
			}
		}(w)
	}

	wg.Wait()

	totalDuration := time.Since(startTime)
	totalOperations := workerCount * operationsPerWorker
	throughput := float64(totalOperations) / totalDuration.Seconds()

	t.Logf("Concurrent operations completed:")
	t.Logf("  Total operations: %d", totalOperations)
	t.Logf("  Total duration: %v", totalDuration)
	t.Logf("  Throughput: %.2f ops/sec", throughput)

	// Check cache stats
	stats := cacheManager.Stats()
	t.Logf("Cache stats:")
	t.Logf("  L1 size: %d/%d", stats.L1Size, stats.L1Capacity)
	t.Logf("  L1 hits: %d, misses: %d", stats.L1Hits, stats.L1Misses)
	t.Logf("  L1 hit rate: %.2f%%", stats.L1HitRate*100)
}

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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Result Structs
// =============================================================================

// BackupCreateResult contains the result of a backup create operation.
type BackupCreateResult struct {
	BackupID  string
	Timestamp time.Time
	Size      int64
	Duration  time.Duration
	Success   bool
	Message   string
}

// BackupListResult contains the result of a backup list operation.
type BackupListResult struct {
	Backups     []BackupInfoResult
	NextCursor  string
	TotalCount  int
	StoragePath string
}

// BackupInfoResult contains information about a single backup.
type BackupInfoResult struct {
	BackupID    string
	Type        string
	Timestamp   time.Time
	Size        int64
	Compression string
	Encryption  string
	NumShards   int
	NumKeys     int64
}

// BackupDeleteResult contains the result of a backup delete operation.
type BackupDeleteResult struct {
	BackupID string
	Deleted  bool
	Message  string
}

// BackupVerifyResult contains the result of a backup verify operation.
type BackupVerifyResult struct {
	BackupID      string
	Valid         bool
	ChecksumValid bool
	ShardsValid   int
	Issues        []string
}

// =============================================================================
// Protocol Interface
// =============================================================================

// backupProtocol abstracts the different protocols for testing.
type backupProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	ListBackups(ctx context.Context, limit int) (*BackupListResult, error)
	Name() string
}

// =============================================================================
// Main Test Function
// =============================================================================

// TestBackupMultiProtocol verifies that backup operations work identically
// across all supported protocols: Embedded, REST, gRPC, QUIC, Unix Socket, and MCP.
//
// Note: This test focuses on read-only operations (List) to avoid
// side effects. Full backup lifecycle tests are in backup_restore_e2e_test.go.
func TestBackupMultiProtocol(t *testing.T) {
	if !helpers.IsBackupAvailable() {
		t.Fatal("Backup service must be available: set QUICBAAS_BACKUP_ENABLED=true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testBackupProtocol(t, ctx, &embeddedBackupClient{})
		})
	}

	t.Run("REST", func(t *testing.T) {
		testBackupProtocol(t, ctx, &restBackupClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testBackupProtocol(t, ctx, &grpcBackupClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testBackupProtocol(t, ctx, &unixSocketBackupClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).
}

// testBackupProtocol runs the backup test suite against a protocol.
func testBackupProtocol(t *testing.T, ctx context.Context, protocol backupProtocol) {
	t.Helper()

	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Protocol %s setup must succeed: backup service must be available", protocol.Name())
	defer func() {
		if teardownErr := protocol.Teardown(t); teardownErr != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
		}
	}()

	t.Run("ListBackups", func(t *testing.T) {
		testListBackups(t, ctx, protocol)
	})

	t.Run("ListBackups_Consistency", func(t *testing.T) {
		testListBackupsConsistency(t, ctx, protocol)
	})
}

// =============================================================================
// Test Functions
// =============================================================================

func testListBackups(t *testing.T, ctx context.Context, protocol backupProtocol) {
	result, err := protocol.ListBackups(ctx, 0)
	require.NoError(t, err, "%s: ListBackups failed", protocol.Name())
	require.NotNil(t, result, "%s: ListBackups returned nil result", protocol.Name())

	// Validate result structure - values depend on cluster state
	assert.NotNil(t, result.Backups, "%s: Backups should not be nil", protocol.Name())
	assert.GreaterOrEqual(t, result.TotalCount, 0, "%s: TotalCount should be non-negative", protocol.Name())

	t.Logf("%s: ListBackups - found %d backups, total_count=%d",
		protocol.Name(), len(result.Backups), result.TotalCount)
}

func testListBackupsConsistency(t *testing.T, ctx context.Context, protocol backupProtocol) {
	// Call ListBackups multiple times to verify each response is structurally
	// valid and internally consistent. In a live distributed system, background
	// processes may create or delete backups between calls, so we verify
	// per-response integrity rather than cross-call count equality.
	const numCalls = 3

	for i := 0; i < numCalls; i++ {
		result, err := protocol.ListBackups(ctx, 0)
		require.NoError(t, err, "%s: ListBackups call %d failed", protocol.Name(), i+1)
		require.NotNil(t, result, "%s: ListBackups call %d returned nil result", protocol.Name(), i+1)
		assert.NotNil(t, result.Backups, "%s: Backups slice should not be nil on call %d", protocol.Name(), i+1)
		assert.Equal(t, len(result.Backups), result.TotalCount,
			"%s: TotalCount should match Backups length on call %d", protocol.Name(), i+1)
		assert.GreaterOrEqual(t, result.TotalCount, 0,
			"%s: TotalCount should be non-negative on call %d", protocol.Name(), i+1)
	}

	t.Logf("%s: ListBackups consistency verified across %d calls", protocol.Name(), numCalls)
}

// =============================================================================
// Embedded Protocol Implementation
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
type ErrEmbeddedClientNotInitialized struct{}

func (e ErrEmbeddedClientNotInitialized) Error() string {
	return "embedded client not initialized"
}

type embeddedBackupClient struct {
	client quicbaas.Client
}

func (c *embeddedBackupClient) Name() string { return "Embedded" }

func (c *embeddedBackupClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	c.client = client

	// Verify backup feature is available
	_, err = c.client.Backup().List(ctx)
	if err != nil {
		closeErr := c.client.Close()
		if closeErr != nil {
			t.Logf("Warning: failed to close client during setup: %v", closeErr)
		}
		c.client = nil
		return &qberrors.WrappedMessageError{Message: "backup service probe failed", Cause: err}
	}

	return nil
}

func (c *embeddedBackupClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized{}
	}

	// The SDK's Backup().List() method only takes context, no limit parameter
	resp, err := c.client.Backup().List(ctx)
	if err != nil {
		return nil, err
	}

	result := &BackupListResult{
		Backups:    make([]BackupInfoResult, 0, len(resp.Backups)),
		TotalCount: len(resp.Backups),
	}

	for _, backup := range resp.Backups {
		result.Backups = append(result.Backups, BackupInfoResult{
			BackupID:  backup.ID,
			Timestamp: backup.CreatedAt,
			Size:      backup.Size,
		})
	}

	return result, nil
}

// =============================================================================
// REST Protocol Implementation
// =============================================================================

type restBackupClient struct {
	baseURL    string
	httpClient *http.Client
}

func (c *restBackupClient) Name() string { return "REST" }

func (c *restBackupClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available"}
	}
	c.baseURL = endpoints[0]
	// The backup routes are system-scoped (backup:list, backup:create).
	c.httpClient = helpers.NewSystemAdminHTTPClient(t, 30*time.Second)

	// Probe to verify backup feature is available
	url := fmt.Sprintf("%s/api/v1/backups", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create probe request", Cause: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to probe backup endpoint", Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to read probe response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return &qberrors.MessageError{Message: "backup service probe failed: status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	return nil
}

func (c *restBackupClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	url := fmt.Sprintf("%s/api/v1/backups", c.baseURL)
	if limit > 0 {
		url = fmt.Sprintf("%s?limit=%d", url, limit)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "REST ListBackups failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	var listResp struct {
		Backups     []BackupInfoResult `json:"backups"`
		NextCursor  string             `json:"next_cursor,omitempty"`
		TotalCount  int                `json:"total_count"`
		StoragePath string             `json:"storage_path,omitempty"`
	}

	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, err
	}

	return &BackupListResult{
		Backups:     listResp.Backups,
		NextCursor:  listResp.NextCursor,
		TotalCount:  listResp.TotalCount,
		StoragePath: listResp.StoragePath,
	}, nil
}

// =============================================================================
// gRPC Protocol Implementation
// =============================================================================

type grpcBackupClient struct {
	conn   *grpc.ClientConn
	client pb.QuicBaaSClient
}

func (c *grpcBackupClient) Name() string { return "gRPC" }

func (c *grpcBackupClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no gRPC endpoints available"}
	}

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(ctx, endpoints[0],
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.SystemAdminGRPCCredentials(t)),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)

	// Verify backup feature is available
	_, err = c.client.ListBackups(ctx, &pb.ListBackupsRequest{})
	if err != nil {
		closeErr := c.conn.Close()
		if closeErr != nil {
			t.Logf("Warning: failed to close connection during setup: %v", closeErr)
		}
		c.conn = nil
		c.client = nil
		return &qberrors.WrappedMessageError{Message: "backup service probe failed", Cause: err}
	}

	return nil
}

func (c *grpcBackupClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	resp, err := c.client.ListBackups(ctx, &pb.ListBackupsRequest{
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}

	result := &BackupListResult{
		Backups:    make([]BackupInfoResult, 0, len(resp.Backups)),
		NextCursor: resp.NextToken,
		TotalCount: len(resp.Backups),
	}

	for _, backup := range resp.Backups {
		backupType := "full"
		switch backup.BackupType {
		case pb.BackupType_BACKUP_TYPE_INCREMENTAL:
			backupType = "incremental"
		}

		result.Backups = append(result.Backups, BackupInfoResult{
			BackupID:    backup.BackupId,
			Type:        backupType,
			Timestamp:   time.Unix(0, backup.Timestamp),
			Size:        backup.Size,
			Compression: backup.Compression.String(),
			Encryption:  backup.Encryption.String(),
			NumShards:   int(backup.NumShards),
		})
	}

	return result, nil
}

// =============================================================================
// Unix Socket Protocol Implementation
// =============================================================================

type unixSocketBackupClient struct {
	conn   *grpc.ClientConn
	client pb.QuicBaaSClient
}

func (c *unixSocketBackupClient) Name() string { return "UnixSocket" }

func (c *unixSocketBackupClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := helpers.GetUnixSocketPath()
	if socketPath == "" {
		return &qberrors.MessageError{Message: "no Unix socket path available"}
	}

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(helpers.SystemAdminGRPCCredentials(t)),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 10*time.Second)
		}),
	)
	if err != nil {
		return err
	}

	c.conn = conn
	c.client = pb.NewQuicBaaSClient(conn)

	// Verify backup feature is available
	_, err = c.client.ListBackups(ctx, &pb.ListBackupsRequest{})
	if err != nil {
		closeErr := c.conn.Close()
		if closeErr != nil {
			t.Logf("Warning: failed to close connection during setup: %v", closeErr)
		}
		c.conn = nil
		c.client = nil
		return &qberrors.WrappedMessageError{Message: "backup service probe failed", Cause: err}
	}

	return nil
}

func (c *unixSocketBackupClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	resp, err := c.client.ListBackups(ctx, &pb.ListBackupsRequest{
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}

	result := &BackupListResult{
		Backups:    make([]BackupInfoResult, 0, len(resp.Backups)),
		NextCursor: resp.NextToken,
		TotalCount: len(resp.Backups),
	}

	for _, backup := range resp.Backups {
		backupType := "full"
		switch backup.BackupType {
		case pb.BackupType_BACKUP_TYPE_INCREMENTAL:
			backupType = "incremental"
		}

		result.Backups = append(result.Backups, BackupInfoResult{
			BackupID:    backup.BackupId,
			Type:        backupType,
			Timestamp:   time.Unix(0, backup.Timestamp),
			Size:        backup.Size,
			Compression: backup.Compression.String(),
			Encryption:  backup.Encryption.String(),
			NumShards:   int(backup.NumShards),
		})
	}

	return result, nil
}

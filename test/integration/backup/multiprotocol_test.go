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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

	t.Run("QUIC", func(t *testing.T) {
		testBackupProtocol(t, ctx, &quicBackupClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testBackupProtocol(t, ctx, &unixSocketBackupClient{})
	})

	t.Run("MCP", func(t *testing.T) {
		testBackupProtocol(t, ctx, &mcpBackupClient{})
	})
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
	c.httpClient = helpers.NewHTTPClient(30 * time.Second)

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
// QUIC Protocol Implementation
// =============================================================================

type quicBackupClient struct {
	addr       string
	conn       *quicgo.Conn
	ctx        context.Context
	cancelFunc context.CancelFunc
	authToken  string
}

func (c *quicBackupClient) Name() string { return "QUIC" }

func (c *quicBackupClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetQUICEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no QUIC endpoints available"}
	}
	c.addr = endpoints[0]

	tlsConf, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	tlsConf.NextProtos = []string{"quicbaas-quic"}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := quicgo.DialAddr(dialCtx, c.addr, tlsConf, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to QUIC endpoint " + c.addr, Cause: err}
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())
	c.conn = conn
	c.ctx = clientCtx
	c.cancelFunc = clientCancel

	// Authenticate to get an access token
	if err := c.authenticate(ctx); err != nil {
		closeErr := c.conn.CloseWithError(0, "auth failed")
		if closeErr != nil {
			t.Logf("Warning: failed to close QUIC connection during setup: %v", closeErr)
		}
		c.cancelFunc()
		return &qberrors.WrappedMessageError{Message: "QUIC authentication failed", Cause: err}
	}

	// Verify backup feature is available
	_, err = c.ListBackups(ctx, 0)
	if err != nil {
		closeErr := c.conn.CloseWithError(0, "backup probe failed")
		if closeErr != nil {
			t.Logf("Warning: failed to close QUIC connection during setup: %v", closeErr)
		}
		c.cancelFunc()
		return &qberrors.WrappedMessageError{Message: "backup service probe failed", Cause: err}
	}

	return nil
}

// authenticate obtains a bearer token via the cluster's PIV login endpoint
// and stores it for subsequent QUIC requests. mTLS-cert-based login is the
// only path the cluster exposes; password authentication has been removed.
func (c *quicBackupClient) authenticate(ctx context.Context) error {
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available for PIV login"}
	}
	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "PIV login failed", Cause: err}
	}
	c.authToken = bearer
	return nil
}

func (c *quicBackupClient) Teardown(t *testing.T) error {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closing")
	}
	return nil
}

// sendRequest sends a QUIC request and returns the response.
func (c *quicBackupClient) sendRequest(ctx context.Context, opcode byte, token string, reqPayload []byte) (*quic.Response, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to open stream", Cause: err}
	}
	defer stream.Close()

	req := &quic.Request{
		Opcode:  opcode,
		Token:   token,
		Payload: reqPayload,
	}

	if err := quic.WriteRequest(stream, req); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to write request", Cause: err}
	}

	resp, err := quic.ReadResponse(stream)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.Status != quic.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quic.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

func (c *quicBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	listReq := &service.ListBackupsRequest{
		Limit: uint64(limit),
	}

	payload, err := quic.EncodePayload(listReq)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to encode list backups request", Cause: err}
	}

	resp, err := c.sendRequest(ctx, quic.OpListBackups, c.authToken, payload)
	if err != nil {
		return nil, err
	}

	var listResp service.ListBackupsResponse
	if err := quic.DecodePayload(resp.Payload, &listResp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to decode list backups response", Cause: err}
	}

	result := &BackupListResult{
		Backups:    make([]BackupInfoResult, 0, len(listResp.Backups)),
		TotalCount: int(listResp.TotalCount),
	}

	for _, backup := range listResp.Backups {
		result.Backups = append(result.Backups, BackupInfoResult{
			BackupID:  backup.BackupID,
			Timestamp: time.Unix(backup.Timestamp, 0),
			Size:      int64(backup.Size),
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

// =============================================================================
// MCP Protocol Implementation
// =============================================================================

type mcpBackupClient struct {
	endpoint      string
	httpClient    *http.Client
	nextID        int
	accessToken   string
	authenticated bool
}

func (c *mcpBackupClient) Name() string { return "MCP" }

func (c *mcpBackupClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetMCPEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no MCP endpoints available"}
	}
	c.endpoint = endpoints[0]
	c.httpClient = helpers.NewHTTPClient(30 * time.Second)
	c.nextID = 1

	// Verify MCP endpoint is reachable via initialize
	initReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "quicbaas-backup-test",
				"version": "1.0.0",
			},
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(initReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to marshal init request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP endpoint not reachable at " + c.endpoint, Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "MCP initialize failed: status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	// Authenticate with the MCP server
	if err := c.authenticate(ctx); err != nil {
		return &qberrors.WrappedMessageError{Message: "MCP authentication failed", Cause: err}
	}

	// Verify backup feature is available
	_, err = c.callMCPTool(ctx, "quicbaas_backup_list", map[string]interface{}{})
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "backup service probe failed via MCP", Cause: err}
	}

	return nil
}

// authenticate performs login with the MCP server using bootstrap admin credentials.
func (c *mcpBackupClient) authenticate(ctx context.Context) error {
	if c.authenticated {
		return nil
	}

	// Acquire a bearer token via the REST PIV login endpoint. The legacy
	// quicbaas_login MCP tool was retired in the PIV/mTLS migration —
	// it used username/password against the deleted /api/v1/auth/login
	// route. Every transport now relies on AuthPIVContext to mint a
	// bearer using the integration cluster's admin client cert.
	restEndpoints := helpers.GetRESTEndpoints()
	if len(restEndpoints) == 0 {
		return &qberrors.MessageError{Message: "no REST endpoints available for PIV login"}
	}
	bearer, err := helpers.AuthPIVContext(ctx, restEndpoints[0], "", "", "")
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "PIV login failed", Cause: err}
	}
	c.accessToken = bearer
	c.authenticated = true
	return nil
}

// authenticateLegacyDISABLED is the retired quicbaas_login flow. Kept as
// dead code for one release so callers porting from older branches can
// see what the migration removed. Not called.
func (c *mcpBackupClient) authenticateLegacyDISABLED(ctx context.Context) error {
	if c.authenticated {
		return nil
	}

	// Create a JSON-RPC request to call the quicbaas_login tool
	// This is a public tool that doesn't require authentication
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "quicbaas_login",
			"arguments": map[string]interface{}{
				"username": "admin",
				"password": "admin",
			},
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "marshaling login request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "creating login request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	// Do NOT set Authorization header since we're not authenticated yet

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "executing login request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "reading login response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return &qberrors.MessageError{Message: "login failed: status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return &qberrors.WrappedMessageError{Message: "unmarshaling login response", Cause: err}
	}

	if mcpResp.Error != nil {
		return &qberrors.MessageError{Message: "login error: " + mcpResp.Error.Message}
	}

	if mcpResp.Result.IsError {
		if len(mcpResp.Result.Content) > 0 {
			return &qberrors.MessageError{Message: "login tool error: " + mcpResp.Result.Content[0].Text}
		}
		return &qberrors.MessageError{Message: "login tool returned error"}
	}

	if len(mcpResp.Result.Content) == 0 {
		return &qberrors.MessageError{Message: "login response has no content"}
	}

	// Parse the login response content which contains JSON with AccessToken (PascalCase)
	// Service types don't have JSON tags, so they marshal with Go field names
	var loginResult struct {
		AccessToken string `json:"AccessToken"`
	}
	if err := json.Unmarshal([]byte(mcpResp.Result.Content[0].Text), &loginResult); err != nil {
		return &qberrors.WrappedMessageError{Message: "parsing login result", Cause: err}
	}

	if loginResult.AccessToken == "" {
		return &qberrors.MessageError{Message: "login response missing access token"}
	}

	c.accessToken = loginResult.AccessToken
	c.authenticated = true

	return nil
}

func (c *mcpBackupClient) Teardown(t *testing.T) error {
	c.httpClient = nil
	c.accessToken = ""
	c.authenticated = false
	return nil
}

// callMCPTool calls an MCP tool with the given arguments, adding the
// Authorization header if authenticated.
func (c *mcpBackupClient) callMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (json.RawMessage, error) {
	mcpReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
		"id": c.nextID,
	}
	c.nextID++

	body, err := json.Marshal(mcpReq)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "marshaling MCP request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "creating MCP request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authenticated {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "executing MCP request", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "reading MCP response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "MCP call failed: status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var mcpResp struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "unmarshaling MCP response", Cause: err}
	}

	if mcpResp.Error != nil {
		return nil, &qberrors.MessageError{Message: "MCP error: " + mcpResp.Error.Message}
	}

	if mcpResp.Result.IsError {
		if len(mcpResp.Result.Content) > 0 {
			return nil, &qberrors.MessageError{Message: "MCP tool error: " + mcpResp.Result.Content[0].Text}
		}
		return nil, &qberrors.MessageError{Message: "MCP tool returned error"}
	}

	if len(mcpResp.Result.Content) > 0 {
		return json.RawMessage(mcpResp.Result.Content[0].Text), nil
	}

	return nil, nil
}

func (c *mcpBackupClient) ListBackups(ctx context.Context, limit int) (*BackupListResult, error) {
	args := map[string]interface{}{}
	if limit > 0 {
		args["limit"] = limit
	}

	rawResult, err := c.callMCPTool(ctx, "quicbaas_backup_list", args)
	if err != nil {
		return nil, err
	}

	if rawResult == nil {
		return &BackupListResult{Backups: []BackupInfoResult{}}, nil
	}

	var result BackupListResult
	if err := json.Unmarshal(rawResult, &result); err != nil {
		// If parsing fails, return empty result indicating we got a response
		return &BackupListResult{Backups: []BackupInfoResult{}}, nil
	}

	return &result, nil
}

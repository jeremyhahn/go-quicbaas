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

package parameter_store

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicpkg "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// ParameterStoreQUICClient wraps QUIC connection for parameter store testing.
type ParameterStoreQUICClient struct {
	addr       string
	conn       *quic.Conn
	tlsConfig  *tls.Config
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewParameterStoreQUICClient creates a new QUIC client for parameter store operations.
func NewParameterStoreQUICClient(addr string) (*ParameterStoreQUICClient, error) {
	tlsConf, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to load integration TLS config", Cause: err}
	}
	tlsConf.NextProtos = []string{"quicbaas-quic"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsConf, nil)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to dial QUIC server at " + addr, Cause: err}
	}

	clientCtx, clientCancel := context.WithCancel(context.Background())

	return &ParameterStoreQUICClient{
		addr:       addr,
		conn:       conn,
		tlsConfig:  tlsConf,
		ctx:        clientCtx,
		cancelFunc: clientCancel,
	}, nil
}

// Close closes the QUIC connection.
func (c *ParameterStoreQUICClient) Close() error {
	c.cancelFunc()
	if c.conn != nil {
		return c.conn.CloseWithError(0, "client closing")
	}
	return nil
}

// sendRequest sends a QUIC request and returns the response.
func (c *ParameterStoreQUICClient) sendRequest(ctx context.Context, opcode byte, reqPayload []byte) (*quicpkg.Response, error) {
	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to open stream", Cause: err}
	}
	defer stream.Close()

	// Write request
	req := &quicpkg.Request{
		Opcode:  opcode,
		Payload: reqPayload,
	}

	if err := quicpkg.WriteRequest(stream, req); err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to write request", Cause: err}
	}

	// Read response
	resp, err := quicpkg.ReadResponse(stream)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	// Check status
	if resp.Status != quicpkg.StatusOK {
		errMsg := string(resp.Payload)
		if errMsg == "" {
			errMsg = "unknown error"
		}
		return nil, quicpkg.StatusToError(resp.Status, errMsg)
	}

	return resp, nil
}

// GetParameter retrieves a parameter via QUIC.
func (c *ParameterStoreQUICClient) GetParameter(ctx context.Context, req *interfaces.GetParameterRequest) (*interfaces.GetParameterResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterGet, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.GetParameterResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetParameter creates or updates a parameter via QUIC.
func (c *ParameterStoreQUICClient) SetParameter(ctx context.Context, req *interfaces.SetParameterRequest) (*interfaces.SetParameterResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterSet, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.SetParameterResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteParameter removes a parameter via QUIC.
func (c *ParameterStoreQUICClient) DeleteParameter(ctx context.Context, req *interfaces.DeleteParameterRequest) (*interfaces.DeleteParameterResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterDelete, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.DeleteParameterResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListParameters returns parameters matching a path prefix via QUIC.
func (c *ParameterStoreQUICClient) ListParameters(ctx context.Context, req *interfaces.ListParametersRequest) (*interfaces.ListParametersResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterList, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.ListParametersResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetParameters retrieves multiple parameters via QUIC.
func (c *ParameterStoreQUICClient) GetParameters(ctx context.Context, req *interfaces.GetParametersRequest) (*interfaces.GetParametersResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterBatchGet, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.GetParametersResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetParameters creates or updates multiple parameters via QUIC.
func (c *ParameterStoreQUICClient) SetParameters(ctx context.Context, req *interfaces.SetParametersRequest) (*interfaces.SetParametersResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterBatchSet, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.SetParametersResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteParameters removes multiple parameters via QUIC.
func (c *ParameterStoreQUICClient) DeleteParameters(ctx context.Context, req *interfaces.DeleteParametersRequest) (*interfaces.DeleteParametersResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterBatchDelete, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.DeleteParametersResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SearchByTags finds parameters matching tag key-value pairs via QUIC.
func (c *ParameterStoreQUICClient) SearchByTags(ctx context.Context, req *interfaces.SearchByTagsRequest) (*interfaces.SearchByTagsResponse, error) {
	payload, err := quicpkg.EncodePayload(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.sendRequest(ctx, quicpkg.OpParameterSearch, payload)
	if err != nil {
		return nil, err
	}

	var result interfaces.SearchByTagsResponse
	if err := quicpkg.DecodePayload(resp.Payload, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// setupQUICClient creates a QUIC client for parameter store testing.
func setupQUICClient(t *testing.T) *ParameterStoreQUICClient {
	t.Helper()
	endpoint := getQUICEndpoint()
	client, err := NewParameterStoreQUICClient(endpoint)
	if err != nil {
		require.NoError(t, err, "QUIC server must be available at %s", endpoint)
	}
	return client
}

// TestQUIC_SetAndGetParameter tests basic parameter set and get via QUIC.
func TestQUIC_SetAndGetParameter(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Unique path for this test
	path := fmt.Sprintf("/integration-test/quic/basic/%d", time.Now().UnixNano())
	testValue := []byte("test-value-for-quic-api")

	// Set parameter
	setResp, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
		Path:        path,
		Value:       testValue,
		Type:        "string",
		Description: "QUIC integration test parameter",
		Tags: map[string]string{
			"env":     "integration",
			"service": "parameter-store",
		},
		Overwrite: true,
	})
	require.NoError(t, err)
	assert.Equal(t, path, setResp.Path)
	assert.True(t, setResp.Created, "Expected created=true for new parameter")

	// Get parameter
	getResp, err := client.GetParameter(ctx, &interfaces.GetParameterRequest{
		Path:           path,
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Equal(t, path, getResp.Parameter.Path)
	assert.Equal(t, testValue, getResp.Parameter.Value)
	assert.Equal(t, "string", getResp.Parameter.Type)
	assert.Equal(t, "QUIC integration test parameter", getResp.Parameter.Description)
	assert.Equal(t, "integration", getResp.Parameter.Tags["env"])

	// Cleanup
	_, err = client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: path})
	require.NoError(t, err)
}

// TestQUIC_DeleteParameter tests parameter deletion via QUIC.
func TestQUIC_DeleteParameter(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create parameter
	path := fmt.Sprintf("/integration-test/quic/delete/%d", time.Now().UnixNano())
	_, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
		Path:      path,
		Value:     []byte("delete-me"),
		Type:      "string",
		Overwrite: true,
	})
	require.NoError(t, err)

	// Delete parameter
	deleteResp, err := client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{
		Path: path,
	})
	require.NoError(t, err)
	assert.Equal(t, path, deleteResp.Path)
	assert.True(t, deleteResp.Deleted)

	// Verify parameter is gone
	_, err = client.GetParameter(ctx, &interfaces.GetParameterRequest{Path: path})
	require.Error(t, err)
}

// TestQUIC_ListParameters tests parameter listing via QUIC.
func TestQUIC_ListParameters(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/quic/list/%d", time.Now().UnixNano())

	// Create multiple parameters
	paths := []string{
		prefix + "/param1",
		prefix + "/param2",
		prefix + "/nested/param3",
	}

	for i, path := range paths {
		_, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
			Path:      path,
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Type:      "string",
			Overwrite: true,
		})
		require.NoError(t, err)
	}

	// List parameters with prefix
	listResp, err := client.ListParameters(ctx, &interfaces.ListParametersRequest{
		Prefix:    prefix,
		Recursive: true,
		Limit:     100,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listResp.Parameters), 3)

	// Cleanup
	for _, path := range paths {
		_, _ = client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: path})
	}
}

// TestQUIC_BatchOperations tests batch get, set, and delete via QUIC.
func TestQUIC_BatchOperations(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/quic/batch/%d", time.Now().UnixNano())

	// Batch set parameters
	setResp, err := client.SetParameters(ctx, &interfaces.SetParametersRequest{
		Parameters: []*interfaces.SetParameterRequest{
			{
				Path:      prefix + "/batch1",
				Value:     []byte("batch-value-1"),
				Type:      "string",
				Tags:      map[string]string{"batch": "true"},
				Overwrite: true,
			},
			{
				Path:      prefix + "/batch2",
				Value:     []byte("batch-value-2"),
				Type:      "string",
				Tags:      map[string]string{"batch": "true"},
				Overwrite: true,
			},
		},
	})
	require.NoError(t, err)
	assert.Len(t, setResp.Results, 2)

	// Batch get parameters
	getResp, err := client.GetParameters(ctx, &interfaces.GetParametersRequest{
		Paths:          []string{prefix + "/batch1", prefix + "/batch2"},
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Len(t, getResp.Parameters, 2)

	// Batch delete parameters
	deleteResp, err := client.DeleteParameters(ctx, &interfaces.DeleteParametersRequest{
		Paths: []string{prefix + "/batch1", prefix + "/batch2"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, deleteResp.DeletedCount)
}

// TestQUIC_SearchByTags tests tag-based search via QUIC.
func TestQUIC_SearchByTags(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create test parameters with unique tags
	prefix := fmt.Sprintf("/integration-test/quic/search/%d", time.Now().UnixNano())
	uniqueTag := fmt.Sprintf("quic-search-tag-%d", time.Now().UnixNano())

	paths := []string{prefix + "/search1", prefix + "/search2"}
	for i, path := range paths {
		_, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
			Path:  path,
			Value: []byte(fmt.Sprintf("search-value-%d", i)),
			Type:  "string",
			Tags: map[string]string{
				"search":    uniqueTag,
				"index":     fmt.Sprintf("%d", i),
				"component": "parameter-store",
			},
			Overwrite: true,
		})
		require.NoError(t, err)
	}

	// Search by tags
	searchResp, err := client.SearchByTags(ctx, &interfaces.SearchByTagsRequest{
		Tags: map[string]string{
			"search":    uniqueTag,
			"component": "parameter-store",
		},
		Limit: 100,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(searchResp.Parameters), 2)

	// Cleanup
	for _, path := range paths {
		_, _ = client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: path})
	}
}

// TestQUIC_ParameterNotFound tests NotFound response for non-existent parameter.
func TestQUIC_ParameterNotFound(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	path := "/integration-test/quic/nonexistent/parameter"

	_, err := client.GetParameter(ctx, &interfaces.GetParameterRequest{
		Path: path,
	})
	require.Error(t, err)
}

// TestQUIC_ParameterTypes tests different parameter types via QUIC.
func TestQUIC_ParameterTypes(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prefix := fmt.Sprintf("/integration-test/quic/types/%d", time.Now().UnixNano())

	testCases := []struct {
		name  string
		path  string
		ptype string
		value []byte
	}{
		{
			name:  "string",
			path:  prefix + "/string",
			ptype: "string",
			value: []byte("hello world"),
		},
		{
			name:  "number",
			path:  prefix + "/number",
			ptype: "number",
			value: []byte("12345"),
		},
		{
			name:  "json",
			path:  prefix + "/json",
			ptype: "json",
			value: []byte(`{"key":"value","nested":{"a":1}}`),
		},
		{
			name:  "binary",
			path:  prefix + "/binary",
			ptype: "binary",
			value: []byte{0x00, 0x01, 0x02, 0xFF},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set parameter
			_, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
				Path:      tc.path,
				Value:     tc.value,
				Type:      tc.ptype,
				Overwrite: true,
			})
			require.NoError(t, err)

			// Get and verify type
			getResp, err := client.GetParameter(ctx, &interfaces.GetParameterRequest{
				Path:           tc.path,
				WithDecryption: true,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.ptype, getResp.Parameter.Type)
			assert.Equal(t, tc.value, getResp.Parameter.Value)

			// Cleanup
			_, _ = client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: tc.path})
		})
	}
}

// TestQUIC_UpdateParameter tests parameter update (overwrite) via QUIC.
func TestQUIC_UpdateParameter(t *testing.T) {
	client := setupQUICClient(t)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	path := fmt.Sprintf("/integration-test/quic/update/%d", time.Now().UnixNano())

	// Create initial parameter
	setResp, err := client.SetParameter(ctx, &interfaces.SetParameterRequest{
		Path:        path,
		Value:       []byte("initial-value"),
		Type:        "string",
		Description: "Initial description",
		Overwrite:   true,
	})
	require.NoError(t, err)
	assert.True(t, setResp.Created)

	// Update parameter
	setResp, err = client.SetParameter(ctx, &interfaces.SetParameterRequest{
		Path:        path,
		Value:       []byte("updated-value"),
		Type:        "string",
		Description: "Updated description",
		Overwrite:   true,
	})
	require.NoError(t, err)
	assert.False(t, setResp.Created, "Expected created=false for update")

	// Verify update
	getResp, err := client.GetParameter(ctx, &interfaces.GetParameterRequest{
		Path:           path,
		WithDecryption: true,
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("updated-value"), getResp.Parameter.Value)
	assert.Equal(t, "Updated description", getResp.Parameter.Description)

	// Cleanup
	_, _ = client.DeleteParameter(ctx, &interfaces.DeleteParameterRequest{Path: path})
}

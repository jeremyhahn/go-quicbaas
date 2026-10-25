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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
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

// SetParameterResult contains the result of a set parameter operation.
type SetParameterResult struct {
	Path      string    `json:"path"`
	Created   bool      `json:"created"`
	Timestamp time.Time `json:"timestamp"`
}

// GetParameterResult contains the result of a get parameter operation.
type GetParameterResult struct {
	Parameter *ParameterItem `json:"parameter"`
	Timestamp time.Time      `json:"timestamp"`
}

// DeleteParameterResult contains the result of a delete parameter operation.
type DeleteParameterResult struct {
	Path      string    `json:"path"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// ListParametersResult contains the result of a list parameters operation.
type ListParametersResult struct {
	Parameters []*ParameterItem `json:"parameters"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
	TotalCount int64            `json:"total_count,omitempty"`
	Timestamp  time.Time        `json:"timestamp"`
}

// SetParametersResult contains the result of a batch set operation.
type SetParametersResult struct {
	Results   []*SetParameterItemResult `json:"results"`
	Timestamp time.Time                 `json:"timestamp"`
}

// SetParameterItemResult contains the result of setting a single parameter in a batch.
type SetParameterItemResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetParametersResult contains the result of a batch get operation.
type GetParametersResult struct {
	Parameters []*ParameterItem  `json:"parameters"`
	Errors     map[string]string `json:"errors,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
}

// DeleteParametersResult contains the result of a batch delete operation.
type DeleteParametersResult struct {
	Results      []*DeleteParameterItemResult `json:"results"`
	DeletedCount int                          `json:"deleted_count"`
	Timestamp    time.Time                    `json:"timestamp"`
}

// DeleteParameterItemResult contains the result of deleting a single parameter in a batch.
type DeleteParameterItemResult struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
	Error   string `json:"error,omitempty"`
}

// SearchParametersResult contains the result of a search by tags operation.
type SearchParametersResult struct {
	Parameters []*ParameterItem `json:"parameters"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
	Timestamp  time.Time        `json:"timestamp"`
}

// ParameterItem represents a parameter with its value and metadata.
type ParameterItem struct {
	Path        string            `json:"path"`
	Value       []byte            `json:"value,omitempty"`
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	IsEncrypted bool              `json:"is_encrypted"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// =============================================================================
// Protocol Interface
// =============================================================================

// parameterStoreProtocol abstracts the different protocols for testing.
type parameterStoreProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	SetParameter(ctx context.Context, path string, value []byte, paramType string, description string, tags map[string]string, overwrite bool) (*SetParameterResult, error)
	GetParameter(ctx context.Context, path string, withDecryption bool) (*GetParameterResult, error)
	DeleteParameter(ctx context.Context, path string) (*DeleteParameterResult, error)
	ListParameters(ctx context.Context, prefix string, recursive bool, limit int) (*ListParametersResult, error)
	SetParameters(ctx context.Context, params []*ParameterItem) (*SetParametersResult, error)
	GetParameters(ctx context.Context, paths []string, withDecryption bool) (*GetParametersResult, error)
	DeleteParameters(ctx context.Context, paths []string) (*DeleteParametersResult, error)
	SearchParameters(ctx context.Context, tags map[string]string, prefix string, limit int) (*SearchParametersResult, error)
	Name() string
}

// =============================================================================
// Main Test Function
// =============================================================================

// TestParameterStoreMultiProtocol verifies that Parameter Store operations work identically
// across all supported protocols: REST, gRPC, Unix Socket, MCP, and Embedded.
func TestParameterStoreMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Test each protocol independently
	t.Run("REST", func(t *testing.T) {
		testParameterStoreProtocol(t, ctx, &restProtocolClient{})
	})

	t.Run("gRPC", func(t *testing.T) {
		testParameterStoreProtocol(t, ctx, &grpcProtocolClient{})
	})

	t.Run("UnixSocket", func(t *testing.T) {
		testParameterStoreProtocol(t, ctx, &unixSocketProtocolClient{})
	})

	// NOTE: no MCP leg here. quicbaas retired its bespoke JSON-RPC MCP
	// listener in favor of go-qrdb's unified multi-protocol server; the
	// unified server's MCP surface has no finalized docker endpoint
	// convention yet, so this suite does not exercise it (deferred).

	if helpers.IsEmbeddedAvailable() {
		t.Run("Embedded", func(t *testing.T) {
			testParameterStoreProtocol(t, ctx, &embeddedProtocolClient{})
		})
	}
}

// testParameterStoreProtocol runs the complete Parameter Store test suite against a protocol.
func testParameterStoreProtocol(t *testing.T, ctx context.Context, protocol parameterStoreProtocol) {
	t.Helper()

	// Setup the protocol client
	err := protocol.Setup(t, ctx)
	require.NoError(t, err, "Failed to setup %s protocol", protocol.Name())
	defer func() {
		err := protocol.Teardown(t)
		if err != nil {
			t.Logf("Warning: teardown error for %s: %v", protocol.Name(), err)
		}
	}()

	basePrefix := fmt.Sprintf("/mp-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Test 1: Set and Get Parameter
	t.Run("set_and_get", func(t *testing.T) {
		testSetAndGetParameter(t, ctx, protocol, basePrefix+"/setget")
	})

	// Test 2: Delete Parameter
	t.Run("delete", func(t *testing.T) {
		testDeleteParameter(t, ctx, protocol, basePrefix+"/delete")
	})

	// Test 3: List Parameters
	t.Run("list", func(t *testing.T) {
		testListParameters(t, ctx, protocol, basePrefix+"/list")
	})

	// Test 4: Batch Operations
	t.Run("batch", func(t *testing.T) {
		testBatchOperations(t, ctx, protocol, basePrefix+"/batch")
	})

	// Test 5: Search by Tags
	t.Run("search", func(t *testing.T) {
		testSearchByTags(t, ctx, protocol, basePrefix+"/search")
	})

	// Test 6: Overwrite Behavior
	t.Run("overwrite", func(t *testing.T) {
		testOverwriteBehavior(t, ctx, protocol, basePrefix+"/overwrite")
	})

	// Test 7: Concurrent Operations
	t.Run("concurrent", func(t *testing.T) {
		testConcurrentOperations(t, ctx, protocol, basePrefix+"/concurrent")
	})

	// Reconnect the protocol client after the concurrent test to reset any
	// corrupted connection state (used by protocols with stateful connections).
	if reconnectable, ok := protocol.(interface{ Reconnect(context.Context) error }); ok {
		if err := reconnectable.Reconnect(ctx); err != nil {
			t.Logf("Warning: Failed to reconnect %s after concurrent test: %v", protocol.Name(), err)
		}
	}

	// Test 8: Error Handling
	t.Run("errors", func(t *testing.T) {
		testErrorHandling(t, ctx, protocol, basePrefix+"/errors")
	})
}

// =============================================================================
// Individual Test Functions
// =============================================================================

func testSetAndGetParameter(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	path := prefix + "/basic-param"
	value := []byte("test-value-for-multiprotocol")
	description := "Multiprotocol integration test parameter"
	tags := map[string]string{
		"env":      "integration",
		"protocol": protocol.Name(),
	}

	// Set parameter
	setResult, err := protocol.SetParameter(ctx, path, value, "string", description, tags, true)
	require.NoError(t, err, "SetParameter failed for %s", protocol.Name())
	assert.NotNil(t, setResult)
	assert.Equal(t, path, setResult.Path)
	assert.True(t, setResult.Created, "Expected created=true for new parameter")

	// Get parameter
	getResult, err := protocol.GetParameter(ctx, path, true)
	require.NoError(t, err, "GetParameter failed for %s", protocol.Name())
	assert.NotNil(t, getResult)
	assert.NotNil(t, getResult.Parameter)
	assert.Equal(t, path, getResult.Parameter.Path)
	assert.Equal(t, value, getResult.Parameter.Value)
	assert.Equal(t, "string", getResult.Parameter.Type)
	assert.Equal(t, description, getResult.Parameter.Description)
	assert.Equal(t, "integration", getResult.Parameter.Tags["env"])
	assert.Equal(t, protocol.Name(), getResult.Parameter.Tags["protocol"])

	// Cleanup
	_, _ = protocol.DeleteParameter(ctx, path)

	t.Logf("[%s] Set and get parameter: %s", protocol.Name(), path)
}

func testDeleteParameter(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	path := prefix + "/delete-param"
	value := []byte("delete-me")

	// Create parameter
	_, err := protocol.SetParameter(ctx, path, value, "string", "", nil, true)
	require.NoError(t, err)

	// Delete parameter
	deleteResult, err := protocol.DeleteParameter(ctx, path)
	require.NoError(t, err, "DeleteParameter failed for %s", protocol.Name())
	assert.NotNil(t, deleteResult)
	assert.Equal(t, path, deleteResult.Path)
	assert.True(t, deleteResult.Deleted)

	// Verify parameter is gone
	_, err = protocol.GetParameter(ctx, path, true)
	assert.Error(t, err, "Expected error when getting deleted parameter")

	t.Logf("[%s] Deleted parameter: %s", protocol.Name(), path)
}

func testListParameters(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	// Create multiple parameters
	paths := []string{
		prefix + "/param1",
		prefix + "/param2",
		prefix + "/nested/param3",
	}

	for i, path := range paths {
		_, err := protocol.SetParameter(ctx, path, []byte(fmt.Sprintf("value-%d", i)), "string", "", nil, true)
		require.NoError(t, err)
	}

	// List parameters with prefix
	listResult, err := protocol.ListParameters(ctx, prefix, true, 100)
	require.NoError(t, err, "ListParameters failed for %s", protocol.Name())
	assert.NotNil(t, listResult)
	assert.GreaterOrEqual(t, len(listResult.Parameters), 3)

	// Verify all paths are present
	foundPaths := make(map[string]bool)
	for _, param := range listResult.Parameters {
		foundPaths[param.Path] = true
	}
	for _, path := range paths {
		assert.True(t, foundPaths[path], "Expected path %s in list results", path)
	}

	// Cleanup
	for _, path := range paths {
		_, _ = protocol.DeleteParameter(ctx, path)
	}

	t.Logf("[%s] Listed %d parameters with prefix %s", protocol.Name(), len(listResult.Parameters), prefix)
}

func testBatchOperations(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	// Batch set parameters
	params := []*ParameterItem{
		{
			Path:  prefix + "/batch1",
			Value: []byte("batch-value-1"),
			Type:  "string",
			Tags:  map[string]string{"batch": "true"},
		},
		{
			Path:  prefix + "/batch2",
			Value: []byte("batch-value-2"),
			Type:  "string",
			Tags:  map[string]string{"batch": "true"},
		},
		{
			Path:  prefix + "/batch3",
			Value: []byte("batch-value-3"),
			Type:  "string",
			Tags:  map[string]string{"batch": "true"},
		},
	}

	setResult, err := protocol.SetParameters(ctx, params)
	require.NoError(t, err, "SetParameters failed for %s", protocol.Name())
	assert.NotNil(t, setResult)
	assert.Len(t, setResult.Results, 3)
	for _, result := range setResult.Results {
		assert.True(t, result.Success, "Batch set should succeed for %s", result.Path)
	}

	// Batch get parameters
	paths := []string{prefix + "/batch1", prefix + "/batch2", prefix + "/batch3"}
	getResult, err := protocol.GetParameters(ctx, paths, true)
	require.NoError(t, err, "GetParameters failed for %s", protocol.Name())
	assert.NotNil(t, getResult)
	assert.Len(t, getResult.Parameters, 3)

	// Batch delete parameters
	deleteResult, err := protocol.DeleteParameters(ctx, paths)
	require.NoError(t, err, "DeleteParameters failed for %s", protocol.Name())
	assert.NotNil(t, deleteResult)
	assert.Equal(t, 3, deleteResult.DeletedCount)

	t.Logf("[%s] Batch operations completed: set=%d, get=%d, delete=%d",
		protocol.Name(), len(setResult.Results), len(getResult.Parameters), deleteResult.DeletedCount)
}

func testSearchByTags(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	uniqueTag := fmt.Sprintf("search-tag-%s-%d", protocol.Name(), time.Now().UnixNano())

	// Create test parameters with unique tags
	paths := []string{prefix + "/search1", prefix + "/search2"}
	for i, path := range paths {
		tags := map[string]string{
			"search":    uniqueTag,
			"index":     fmt.Sprintf("%d", i),
			"component": "parameter-store",
		}
		_, err := protocol.SetParameter(ctx, path, []byte(fmt.Sprintf("search-value-%d", i)), "string", "", tags, true)
		require.NoError(t, err)
	}

	// Search by tags
	searchTags := map[string]string{
		"search":    uniqueTag,
		"component": "parameter-store",
	}
	searchResult, err := protocol.SearchParameters(ctx, searchTags, "", 100)
	require.NoError(t, err, "SearchParameters failed for %s", protocol.Name())
	assert.NotNil(t, searchResult)
	assert.GreaterOrEqual(t, len(searchResult.Parameters), 2)

	// Cleanup
	for _, path := range paths {
		_, _ = protocol.DeleteParameter(ctx, path)
	}

	t.Logf("[%s] Search returned %d parameters for tags", protocol.Name(), len(searchResult.Parameters))
}

func testOverwriteBehavior(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	path := prefix + "/overwrite-param"

	// Create initial parameter
	_, err := protocol.SetParameter(ctx, path, []byte("initial-value"), "string", "Initial description", nil, true)
	require.NoError(t, err)

	// Update with overwrite=true
	setResult, err := protocol.SetParameter(ctx, path, []byte("updated-value"), "string", "Updated description", nil, true)
	require.NoError(t, err, "SetParameter with overwrite=true should succeed for %s", protocol.Name())
	assert.False(t, setResult.Created, "Expected created=false for update")

	// Verify update
	getResult, err := protocol.GetParameter(ctx, path, true)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated-value"), getResult.Parameter.Value)
	assert.Equal(t, "Updated description", getResult.Parameter.Description)

	// Cleanup
	_, _ = protocol.DeleteParameter(ctx, path)

	t.Logf("[%s] Overwrite behavior verified for %s", protocol.Name(), path)
}

func testConcurrentOperations(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	concurrency := 5
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			path := fmt.Sprintf("%s/concurrent-%d", prefix, idx)
			value := []byte(fmt.Sprintf("concurrent-value-%d", idx))

			idxStr := strconv.Itoa(idx)

			// Set
			_, err := protocol.SetParameter(ctx, path, value, "string", "", nil, true)
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "set error " + idxStr, Cause: err}
				return
			}

			// Get
			getResult, err := protocol.GetParameter(ctx, path, true)
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "get error " + idxStr, Cause: err}
				return
			}

			if !bytes.Equal(getResult.Parameter.Value, value) {
				errors <- &qberrors.MessageError{Message: "value mismatch " + idxStr}
				return
			}

			// Delete
			_, err = protocol.DeleteParameter(ctx, path)
			if err != nil {
				errors <- &qberrors.WrappedMessageError{Message: "delete error " + idxStr, Cause: err}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
		errorCount++
	}

	assert.Equal(t, 0, errorCount, "All concurrent operations should succeed")
	t.Logf("[%s] Completed %d concurrent operations", protocol.Name(), concurrency)
}

func testErrorHandling(t *testing.T, ctx context.Context, protocol parameterStoreProtocol, prefix string) {
	t.Helper()

	t.Run("get non-existent", func(t *testing.T) {
		path := prefix + "/nonexistent-param"
		_, err := protocol.GetParameter(ctx, path, true)
		assert.Error(t, err)
	})

	t.Run("delete non-existent", func(t *testing.T) {
		path := prefix + "/nonexistent-delete"
		_, err := protocol.DeleteParameter(ctx, path)
		// Some protocols may not return error for deleting non-existent, but result should indicate not deleted
		if err == nil {
			t.Logf("[%s] Delete non-existent did not return error (protocol may allow this)", protocol.Name())
		}
	})

	t.Run("batch get with invalid paths", func(t *testing.T) {
		paths := []string{prefix + "/invalid1", prefix + "/invalid2"}
		result, err := protocol.GetParameters(ctx, paths, true)
		// Should either return error or empty results
		if err == nil {
			assert.NotNil(t, result)
			// Parameters should be empty or errors should be populated
			if len(result.Parameters) > 0 {
				t.Logf("[%s] Batch get returned %d parameters for invalid paths", protocol.Name(), len(result.Parameters))
			}
		}
	})
}

// =============================================================================
// REST Protocol Client
// =============================================================================

type restProtocolClient struct {
	baseURL    string
	httpClient *http.Client
}

func (c *restProtocolClient) Name() string { return "REST" }

func (c *restProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetRESTEndpoints()
	require.NotEmpty(t, endpoints, "REST endpoints must be available for integration tests")
	c.baseURL = endpoints[0]
	c.httpClient = helpers.NewTenantHTTPClient(t, 30*time.Second)
	return nil
}

func (c *restProtocolClient) Teardown(t *testing.T) error {
	return nil
}

func (c *restProtocolClient) SetParameter(ctx context.Context, path string, value []byte, paramType string, description string, tags map[string]string, overwrite bool) (*SetParameterResult, error) {
	reqBody := map[string]interface{}{
		"value":       base64.StdEncoding.EncodeToString(value),
		"type":        paramType,
		"description": description,
		"overwrite":   overwrite,
	}
	if tags != nil {
		reqBody["tags"] = tags
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/parameters%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &SetParameterResult{
		Path:    result["path"].(string),
		Created: result["created"].(bool),
	}, nil
}

func (c *restProtocolClient) GetParameter(ctx context.Context, path string, withDecryption bool) (*GetParameterResult, error) {
	url := fmt.Sprintf("%s/api/v1/parameters%s", c.baseURL, path)
	if withDecryption {
		url += "?with_decryption=true"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	param := result["parameter"].(map[string]interface{})
	valueB64 := param["value"].(string)
	value, err := base64.StdEncoding.DecodeString(valueB64)
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	if tagsRaw, ok := param["tags"].(map[string]interface{}); ok {
		for k, v := range tagsRaw {
			tags[k] = v.(string)
		}
	}

	return &GetParameterResult{
		Parameter: &ParameterItem{
			Path:        param["path"].(string),
			Value:       value,
			Type:        param["type"].(string),
			Description: mpGetStringOrEmpty(param, "description"),
			Tags:        tags,
		},
	}, nil
}

func (c *restProtocolClient) DeleteParameter(ctx context.Context, path string) (*DeleteParameterResult, error) {
	url := fmt.Sprintf("%s/api/v1/parameters%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &DeleteParameterResult{
		Path:    result["path"].(string),
		Deleted: result["deleted"].(bool),
	}, nil
}

func (c *restProtocolClient) ListParameters(ctx context.Context, prefix string, recursive bool, limit int) (*ListParametersResult, error) {
	url := fmt.Sprintf("%s/api/v1/parameters?prefix=%s&recursive=%t&limit=%d", c.baseURL, prefix, recursive, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, 0)
	if paramsRaw, ok := result["parameters"].([]interface{}); ok {
		for _, pRaw := range paramsRaw {
			p := pRaw.(map[string]interface{})
			params = append(params, &ParameterItem{
				Path: p["path"].(string),
				Type: mpGetStringOrEmpty(p, "type"),
			})
		}
	}

	return &ListParametersResult{
		Parameters: params,
	}, nil
}

func (c *restProtocolClient) SetParameters(ctx context.Context, params []*ParameterItem) (*SetParametersResult, error) {
	reqParams := make([]map[string]interface{}, len(params))
	for i, p := range params {
		reqParams[i] = map[string]interface{}{
			"path":      p.Path,
			"value":     base64.StdEncoding.EncodeToString(p.Value),
			"type":      p.Type,
			"tags":      p.Tags,
			"overwrite": true,
		}
	}

	reqBody := map[string]interface{}{
		"parameters": reqParams,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/parameters/batch/set", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	results := make([]*SetParameterItemResult, 0)
	if resultsRaw, ok := result["results"].([]interface{}); ok {
		for _, rRaw := range resultsRaw {
			r := rRaw.(map[string]interface{})
			results = append(results, &SetParameterItemResult{
				Path:    mpGetStringOrEmpty(r, "path"),
				Created: mpGetBoolOrFalse(r, "created"),
				Success: mpGetBoolOrFalse(r, "success"),
			})
		}
	}

	return &SetParametersResult{
		Results: results,
	}, nil
}

func (c *restProtocolClient) GetParameters(ctx context.Context, paths []string, withDecryption bool) (*GetParametersResult, error) {
	reqBody := map[string]interface{}{
		"paths":           paths,
		"with_decryption": withDecryption,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/parameters/batch/get", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, 0)
	if paramsRaw, ok := result["parameters"].([]interface{}); ok {
		for _, pRaw := range paramsRaw {
			p := pRaw.(map[string]interface{})
			value := []byte{}
			if valueB64, ok := p["value"].(string); ok {
				value, _ = base64.StdEncoding.DecodeString(valueB64)
			}
			params = append(params, &ParameterItem{
				Path:  mpGetStringOrEmpty(p, "path"),
				Value: value,
				Type:  mpGetStringOrEmpty(p, "type"),
			})
		}
	}

	return &GetParametersResult{
		Parameters: params,
	}, nil
}

func (c *restProtocolClient) DeleteParameters(ctx context.Context, paths []string) (*DeleteParametersResult, error) {
	reqBody := map[string]interface{}{
		"paths": paths,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/parameters/batch/delete", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	deletedCount := 0
	if count, ok := result["deleted_count"].(float64); ok {
		deletedCount = int(count)
	}

	return &DeleteParametersResult{
		DeletedCount: deletedCount,
	}, nil
}

func (c *restProtocolClient) SearchParameters(ctx context.Context, tags map[string]string, prefix string, limit int) (*SearchParametersResult, error) {
	reqBody := map[string]interface{}{
		"tags":  tags,
		"limit": limit,
	}
	if prefix != "" {
		reqBody["prefix"] = prefix
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/parameters/search", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, 0)
	if paramsRaw, ok := result["parameters"].([]interface{}); ok {
		for _, pRaw := range paramsRaw {
			p := pRaw.(map[string]interface{})
			params = append(params, &ParameterItem{
				Path: mpGetStringOrEmpty(p, "path"),
				Type: mpGetStringOrEmpty(p, "type"),
			})
		}
	}

	return &SearchParametersResult{
		Parameters: params,
	}, nil
}

// =============================================================================
// gRPC Protocol Client
// =============================================================================

type grpcProtocolClient struct {
	conn   *grpc.ClientConn
	client pb.ParameterStoreClient
}

func (c *grpcProtocolClient) Name() string { return "gRPC" }

func (c *grpcProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	require.NotEmpty(t, endpoints, "gRPC endpoints must be available for integration tests")

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx, endpoints[0],
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
	)
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = pb.NewParameterStoreClient(conn)
	return nil
}

func (c *grpcProtocolClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcProtocolClient) SetParameter(ctx context.Context, path string, value []byte, paramType string, description string, tags map[string]string, overwrite bool) (*SetParameterResult, error) {
	resp, err := c.client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:        path,
		Value:       value,
		Type:        pb.ParameterType(pb.ParameterType_value["PARAMETER_TYPE_"+mpToUpperParameterType(paramType)]),
		Description: description,
		Tags:        tags,
		Overwrite:   overwrite,
	})
	if err != nil {
		return nil, err
	}

	return &SetParameterResult{
		Path:    resp.Path,
		Created: resp.Created,
	}, nil
}

func (c *grpcProtocolClient) GetParameter(ctx context.Context, path string, withDecryption bool) (*GetParameterResult, error) {
	resp, err := c.client.GetParameter(ctx, &pb.GetParameterRequest{
		Path:           path,
		WithDecryption: withDecryption,
	})
	if err != nil {
		return nil, err
	}

	return &GetParameterResult{
		Parameter: &ParameterItem{
			Path:        resp.Parameter.Path,
			Value:       resp.Parameter.Value,
			Type:        mpParameterTypeToString(resp.Parameter.Type),
			Description: resp.Parameter.Description,
			Tags:        resp.Parameter.Tags,
		},
	}, nil
}

func (c *grpcProtocolClient) DeleteParameter(ctx context.Context, path string) (*DeleteParameterResult, error) {
	resp, err := c.client.DeleteParameter(ctx, &pb.DeleteParameterRequest{
		Path: path,
	})
	if err != nil {
		return nil, err
	}

	return &DeleteParameterResult{
		Path:    resp.Path,
		Deleted: resp.Deleted,
	}, nil
}

func (c *grpcProtocolClient) ListParameters(ctx context.Context, prefix string, recursive bool, limit int) (*ListParametersResult, error) {
	resp, err := c.client.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix:    prefix,
		Recursive: recursive,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path: p.Path,
			Type: mpParameterTypeToString(p.Type),
		}
	}

	return &ListParametersResult{
		Parameters: params,
	}, nil
}

func (c *grpcProtocolClient) SetParameters(ctx context.Context, params []*ParameterItem) (*SetParametersResult, error) {
	pbParams := make([]*pb.SetParameterItem, len(params))
	for i, p := range params {
		pbParams[i] = &pb.SetParameterItem{
			Path:      p.Path,
			Value:     p.Value,
			Type:      pb.ParameterType(pb.ParameterType_value["PARAMETER_TYPE_"+mpToUpperParameterType(p.Type)]),
			Tags:      p.Tags,
			Overwrite: true,
		}
	}

	resp, err := c.client.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: pbParams,
	})
	if err != nil {
		return nil, err
	}

	results := make([]*SetParameterItemResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = &SetParameterItemResult{
			Path:    r.Path,
			Created: r.Created,
			Success: r.Success,
			Error:   r.Error,
		}
	}

	return &SetParametersResult{
		Results: results,
	}, nil
}

func (c *grpcProtocolClient) GetParameters(ctx context.Context, paths []string, withDecryption bool) (*GetParametersResult, error) {
	resp, err := c.client.GetParameters(ctx, &pb.GetParametersRequest{
		Paths:          paths,
		WithDecryption: withDecryption,
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path:  p.Path,
			Value: p.Value,
			Type:  mpParameterTypeToString(p.Type),
		}
	}

	return &GetParametersResult{
		Parameters: params,
	}, nil
}

func (c *grpcProtocolClient) DeleteParameters(ctx context.Context, paths []string) (*DeleteParametersResult, error) {
	resp, err := c.client.DeleteParameters(ctx, &pb.DeleteParametersRequest{
		Paths: paths,
	})
	if err != nil {
		return nil, err
	}

	return &DeleteParametersResult{
		DeletedCount: int(resp.DeletedCount),
	}, nil
}

func (c *grpcProtocolClient) SearchParameters(ctx context.Context, tags map[string]string, prefix string, limit int) (*SearchParametersResult, error) {
	resp, err := c.client.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags:   tags,
		Prefix: prefix,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path: p.Path,
			Type: mpParameterTypeToString(p.Type),
		}
	}

	return &SearchParametersResult{
		Parameters: params,
	}, nil
}

// =============================================================================
// Unix Socket Protocol Client
// =============================================================================

type unixSocketProtocolClient struct {
	conn   *grpc.ClientConn
	client pb.ParameterStoreClient
}

func (c *unixSocketProtocolClient) Name() string { return "UnixSocket" }

func (c *unixSocketProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetUnixSocketEndpoints()
	require.NotEmpty(t, endpoints, "Unix socket endpoints must be available for integration tests")

	socketPath := endpoints[0]

	// Check if socket exists
	netConn, err := net.Dial("unix", socketPath)
	require.NoError(t, err, "Unix socket must be available at %s", socketPath)
	netConn.Close()

	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
	)
	if err != nil {
		return err
	}
	c.conn = conn
	c.client = pb.NewParameterStoreClient(conn)
	return nil
}

func (c *unixSocketProtocolClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketProtocolClient) SetParameter(ctx context.Context, path string, value []byte, paramType string, description string, tags map[string]string, overwrite bool) (*SetParameterResult, error) {
	resp, err := c.client.SetParameter(ctx, &pb.SetParameterRequest{
		Path:        path,
		Value:       value,
		Type:        pb.ParameterType(pb.ParameterType_value["PARAMETER_TYPE_"+mpToUpperParameterType(paramType)]),
		Description: description,
		Tags:        tags,
		Overwrite:   overwrite,
	})
	if err != nil {
		return nil, err
	}

	return &SetParameterResult{
		Path:    resp.Path,
		Created: resp.Created,
	}, nil
}

func (c *unixSocketProtocolClient) GetParameter(ctx context.Context, path string, withDecryption bool) (*GetParameterResult, error) {
	resp, err := c.client.GetParameter(ctx, &pb.GetParameterRequest{
		Path:           path,
		WithDecryption: withDecryption,
	})
	if err != nil {
		return nil, err
	}

	return &GetParameterResult{
		Parameter: &ParameterItem{
			Path:        resp.Parameter.Path,
			Value:       resp.Parameter.Value,
			Type:        mpParameterTypeToString(resp.Parameter.Type),
			Description: resp.Parameter.Description,
			Tags:        resp.Parameter.Tags,
		},
	}, nil
}

func (c *unixSocketProtocolClient) DeleteParameter(ctx context.Context, path string) (*DeleteParameterResult, error) {
	resp, err := c.client.DeleteParameter(ctx, &pb.DeleteParameterRequest{
		Path: path,
	})
	if err != nil {
		return nil, err
	}

	return &DeleteParameterResult{
		Path:    resp.Path,
		Deleted: resp.Deleted,
	}, nil
}

func (c *unixSocketProtocolClient) ListParameters(ctx context.Context, prefix string, recursive bool, limit int) (*ListParametersResult, error) {
	resp, err := c.client.ListParameters(ctx, &pb.ListParametersRequest{
		Prefix:    prefix,
		Recursive: recursive,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path: p.Path,
			Type: mpParameterTypeToString(p.Type),
		}
	}

	return &ListParametersResult{
		Parameters: params,
	}, nil
}

func (c *unixSocketProtocolClient) SetParameters(ctx context.Context, params []*ParameterItem) (*SetParametersResult, error) {
	pbParams := make([]*pb.SetParameterItem, len(params))
	for i, p := range params {
		pbParams[i] = &pb.SetParameterItem{
			Path:      p.Path,
			Value:     p.Value,
			Type:      pb.ParameterType(pb.ParameterType_value["PARAMETER_TYPE_"+mpToUpperParameterType(p.Type)]),
			Tags:      p.Tags,
			Overwrite: true,
		}
	}

	resp, err := c.client.SetParameters(ctx, &pb.SetParametersRequest{
		Parameters: pbParams,
	})
	if err != nil {
		return nil, err
	}

	results := make([]*SetParameterItemResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = &SetParameterItemResult{
			Path:    r.Path,
			Created: r.Created,
			Success: r.Success,
			Error:   r.Error,
		}
	}

	return &SetParametersResult{
		Results: results,
	}, nil
}

func (c *unixSocketProtocolClient) GetParameters(ctx context.Context, paths []string, withDecryption bool) (*GetParametersResult, error) {
	resp, err := c.client.GetParameters(ctx, &pb.GetParametersRequest{
		Paths:          paths,
		WithDecryption: withDecryption,
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path:  p.Path,
			Value: p.Value,
			Type:  mpParameterTypeToString(p.Type),
		}
	}

	return &GetParametersResult{
		Parameters: params,
	}, nil
}

func (c *unixSocketProtocolClient) DeleteParameters(ctx context.Context, paths []string) (*DeleteParametersResult, error) {
	resp, err := c.client.DeleteParameters(ctx, &pb.DeleteParametersRequest{
		Paths: paths,
	})
	if err != nil {
		return nil, err
	}

	return &DeleteParametersResult{
		DeletedCount: int(resp.DeletedCount),
	}, nil
}

func (c *unixSocketProtocolClient) SearchParameters(ctx context.Context, tags map[string]string, prefix string, limit int) (*SearchParametersResult, error) {
	resp, err := c.client.SearchByTags(ctx, &pb.SearchByTagsRequest{
		Tags:   tags,
		Prefix: prefix,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, err
	}

	params := make([]*ParameterItem, len(resp.Parameters))
	for i, p := range resp.Parameters {
		params[i] = &ParameterItem{
			Path: p.Path,
			Type: mpParameterTypeToString(p.Type),
		}
	}

	return &SearchParametersResult{
		Parameters: params,
	}, nil
}

// =============================================================================
// Embedded Protocol Client
// =============================================================================

// ErrEmbeddedClientNotInitialized is returned when the embedded client is not initialized.
var ErrEmbeddedClientNotInitialized = &qberrors.MessageError{Message: "embedded client not initialized"}

// ErrEmbeddedProtocolNotAvailable is returned when the embedded protocol is not available
// in the integration test context.
var ErrEmbeddedProtocolNotAvailable = &qberrors.MessageError{Message: "embedded protocol not available in integration test context (requires in-process service instance)"}

type embeddedProtocolClient struct {
	client quicbaas.Client
}

func (c *embeddedProtocolClient) Name() string { return "Embedded" }

func (c *embeddedProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc), helpers.EmbeddedSystemIdentity())
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}
	c.client = client
	return nil
}

func (c *embeddedProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedProtocolClient) SetParameter(ctx context.Context, path string, value []byte, paramType string, description string, tags map[string]string, overwrite bool) (*SetParameterResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) GetParameter(ctx context.Context, path string, withDecryption bool) (*GetParameterResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) DeleteParameter(ctx context.Context, path string) (*DeleteParameterResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) ListParameters(ctx context.Context, prefix string, recursive bool, limit int) (*ListParametersResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) SetParameters(ctx context.Context, params []*ParameterItem) (*SetParametersResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) GetParameters(ctx context.Context, paths []string, withDecryption bool) (*GetParametersResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) DeleteParameters(ctx context.Context, paths []string) (*DeleteParametersResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

func (c *embeddedProtocolClient) SearchParameters(ctx context.Context, tags map[string]string, prefix string, limit int) (*SearchParametersResult, error) {
	if c.client == nil {
		return nil, ErrEmbeddedClientNotInitialized
	}
	return nil, ErrEmbeddedClientNotInitialized
}

// =============================================================================
// Helper Functions
// =============================================================================

// mpGetStringOrEmpty safely gets a string from a map or returns empty string.
func mpGetStringOrEmpty(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// mpGetBoolOrFalse safely gets a bool from a map or returns false.
func mpGetBoolOrFalse(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// mpToUpperParameterType converts a parameter type string to uppercase for proto enum.
func mpToUpperParameterType(t string) string {
	switch t {
	case "string":
		return "STRING"
	case "number":
		return "NUMBER"
	case "json":
		return "JSON"
	case "binary":
		return "BINARY"
	default:
		return "STRING"
	}
}

// mpParameterTypeToString converts a proto ParameterType to a string.
func mpParameterTypeToString(t pb.ParameterType) string {
	switch t {
	case pb.ParameterType_PARAMETER_TYPE_STRING:
		return "string"
	case pb.ParameterType_PARAMETER_TYPE_NUMBER:
		return "number"
	case pb.ParameterType_PARAMETER_TYPE_JSON:
		return "json"
	case pb.ParameterType_PARAMETER_TYPE_BINARY:
		return "binary"
	default:
		return "string"
	}
}

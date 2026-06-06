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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// REST API Integration Tests for Parameter Store

// TestREST_SetAndGetParameter tests basic parameter set and get via REST API.
func TestREST_SetAndGetParameter(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	// Unique path for this test
	path := fmt.Sprintf("/integration-test/rest/basic/%d", time.Now().UnixNano())
	testValue := "test-value-for-rest-api"
	encodedValue := base64.StdEncoding.EncodeToString([]byte(testValue))

	// Set parameter
	setReq := map[string]interface{}{
		"value":       encodedValue,
		"type":        "string",
		"description": "Integration test parameter",
		"tags": map[string]string{
			"env":     "integration",
			"service": "parameter-store",
		},
	}
	setBody, err := json.Marshal(setReq)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(setBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated,
		"Expected 200 or 201, got %d", resp.StatusCode)

	// Read response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var setResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &setResp))
	assert.Equal(t, path, setResp["path"])
	assert.True(t, setResp["created"].(bool), "Expected created=true")

	// Get parameter
	req, err = http.NewRequest(http.MethodGet, base+"/api/v1/parameters"+path, nil)
	require.NoError(t, err)

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &getResp))

	param := getResp["parameter"].(map[string]interface{})
	assert.Equal(t, path, param["path"])
	assert.Equal(t, "string", param["type"])
	assert.Equal(t, "Integration test parameter", param["description"])

	// Verify value
	returnedValue, err := base64.StdEncoding.DecodeString(param["value"].(string))
	require.NoError(t, err)
	assert.Equal(t, testValue, string(returnedValue))

	// Verify tags
	tags := param["tags"].(map[string]interface{})
	assert.Equal(t, "integration", tags["env"])
	assert.Equal(t, "parameter-store", tags["service"])

	// Cleanup: Delete parameter
	req, err = http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+path, nil)
	require.NoError(t, err)

	resp, err = client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestREST_DeleteParameter tests parameter deletion via REST API.
func TestREST_DeleteParameter(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	// Create a parameter to delete
	path := fmt.Sprintf("/integration-test/rest/delete/%d", time.Now().UnixNano())
	encodedValue := base64.StdEncoding.EncodeToString([]byte("delete-me"))

	setReq := map[string]interface{}{
		"value": encodedValue,
		"type":  "string",
	}
	setBody, _ := json.Marshal(setReq)

	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(setBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	// Delete parameter
	req, err = http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+path, nil)
	require.NoError(t, err)

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var deleteResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &deleteResp))
	assert.Equal(t, path, deleteResp["path"])
	assert.True(t, deleteResp["deleted"].(bool))

	// Verify parameter is gone
	req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/parameters"+path, nil)
	resp, err = client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestREST_ListParameters tests parameter listing via REST API.
func TestREST_ListParameters(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/rest/list/%d", time.Now().UnixNano())

	// Create multiple parameters
	paths := []string{
		prefix + "/param1",
		prefix + "/param2",
		prefix + "/nested/param3",
	}

	for i, path := range paths {
		encodedValue := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("value-%d", i)))
		setReq := map[string]interface{}{
			"value": encodedValue,
			"type":  "string",
		}
		setBody, _ := json.Marshal(setReq)

		req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(setBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// List parameters with prefix
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/parameters?prefix="+prefix+"&recursive=true", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var listResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &listResp))

	params := listResp["parameters"].([]interface{})
	assert.GreaterOrEqual(t, len(params), 3, "Expected at least 3 parameters")

	// Cleanup
	for _, path := range paths {
		req, _ := http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+path, nil)
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// TestREST_BatchOperations tests batch get, set, and delete via REST API.
func TestREST_BatchOperations(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	// Create test prefix
	prefix := fmt.Sprintf("/integration-test/rest/batch/%d", time.Now().UnixNano())

	// Batch set parameters
	params := []map[string]interface{}{
		{
			"path":  prefix + "/batch1",
			"value": base64.StdEncoding.EncodeToString([]byte("batch-value-1")),
			"type":  "string",
			"tags":  map[string]string{"batch": "true"},
		},
		{
			"path":  prefix + "/batch2",
			"value": base64.StdEncoding.EncodeToString([]byte("batch-value-2")),
			"type":  "string",
			"tags":  map[string]string{"batch": "true"},
		},
	}

	batchSetReq := map[string]interface{}{
		"parameters": params,
	}
	batchSetBody, _ := json.Marshal(batchSetReq)

	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/parameters/batch/set", bytes.NewReader(batchSetBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Batch get parameters
	batchGetReq := map[string]interface{}{
		"paths":           []string{prefix + "/batch1", prefix + "/batch2"},
		"with_decryption": true,
	}
	batchGetBody, _ := json.Marshal(batchGetReq)

	req, err = http.NewRequest(http.MethodPost, base+"/api/v1/parameters/batch/get", bytes.NewReader(batchGetBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var batchGetResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &batchGetResp))

	returnedParams := batchGetResp["parameters"].([]interface{})
	assert.Len(t, returnedParams, 2)

	// Batch delete parameters
	batchDeleteReq := map[string]interface{}{
		"paths": []string{prefix + "/batch1", prefix + "/batch2"},
	}
	batchDeleteBody, _ := json.Marshal(batchDeleteReq)

	req, err = http.NewRequest(http.MethodPost, base+"/api/v1/parameters/batch/delete", bytes.NewReader(batchDeleteBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ = io.ReadAll(resp.Body)
	var batchDeleteResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &batchDeleteResp))

	assert.Equal(t, float64(2), batchDeleteResp["deleted_count"])
}

// TestREST_SearchByTags tests tag-based search via REST API.
func TestREST_SearchByTags(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	// Create test parameters with unique tags
	prefix := fmt.Sprintf("/integration-test/rest/search/%d", time.Now().UnixNano())
	uniqueTag := fmt.Sprintf("search-tag-%d", time.Now().UnixNano())

	paths := []string{prefix + "/search1", prefix + "/search2"}
	for i, path := range paths {
		encodedValue := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("search-value-%d", i)))
		setReq := map[string]interface{}{
			"value": encodedValue,
			"type":  "string",
			"tags": map[string]string{
				"search":    uniqueTag,
				"index":     fmt.Sprintf("%d", i),
				"component": "parameter-store",
			},
		}
		setBody, _ := json.Marshal(setReq)

		req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(setBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
	}

	// Search by tags
	searchReq := map[string]interface{}{
		"tags": map[string]string{
			"search":    uniqueTag,
			"component": "parameter-store",
		},
		"with_values": false,
		"limit":       100,
	}
	searchBody, _ := json.Marshal(searchReq)

	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/parameters/search", bytes.NewReader(searchBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	var searchResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &searchResp))

	params := searchResp["parameters"].([]interface{})
	assert.GreaterOrEqual(t, len(params), 2, "Expected at least 2 parameters")

	// Cleanup
	for _, path := range paths {
		req, _ := http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+path, nil)
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}
}

// TestREST_ParameterNotFound tests 404 response for non-existent parameter.
func TestREST_ParameterNotFound(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	path := "/integration-test/nonexistent/parameter"

	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/parameters"+path, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestREST_ParameterTypes tests different parameter types via REST API.
func TestREST_ParameterTypes(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	prefix := fmt.Sprintf("/integration-test/rest/types/%d", time.Now().UnixNano())

	testCases := []struct {
		name  string
		path  string
		ptype string
		value interface{}
	}{
		{
			name:  "string",
			path:  prefix + "/string",
			ptype: "string",
			value: "hello world",
		},
		{
			name:  "number",
			path:  prefix + "/number",
			ptype: "number",
			value: "12345",
		},
		{
			name:  "json",
			path:  prefix + "/json",
			ptype: "json",
			value: `{"key":"value","nested":{"a":1}}`,
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
			var encodedValue string
			switch v := tc.value.(type) {
			case string:
				encodedValue = base64.StdEncoding.EncodeToString([]byte(v))
			case []byte:
				encodedValue = base64.StdEncoding.EncodeToString(v)
			}

			setReq := map[string]interface{}{
				"value": encodedValue,
				"type":  tc.ptype,
			}
			setBody, _ := json.Marshal(setReq)

			req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+tc.path, bytes.NewReader(setBody))
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			require.NoError(t, err)
			resp.Body.Close()

			// Get and verify type
			req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/parameters"+tc.path, nil)
			resp, err = client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)
			var getResp map[string]interface{}
			require.NoError(t, json.Unmarshal(body, &getResp))

			param := getResp["parameter"].(map[string]interface{})
			assert.Equal(t, tc.ptype, param["type"])

			// Cleanup
			req, _ = http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+tc.path, nil)
			resp, _ = client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
		})
	}
}

// TestREST_UpdateParameter tests parameter update (overwrite) via REST API.
func TestREST_UpdateParameter(t *testing.T) {
	base := getRESTEndpoint()
	client := helpers.NewHTTPClient(30 * time.Second)

	path := fmt.Sprintf("/integration-test/rest/update/%d", time.Now().UnixNano())

	// Create initial parameter
	setReq := map[string]interface{}{
		"value":       base64.StdEncoding.EncodeToString([]byte("initial-value")),
		"type":        "string",
		"description": "Initial description",
	}
	setBody, _ := json.Marshal(setReq)

	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(setBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	// Update parameter
	updateReq := map[string]interface{}{
		"value":       base64.StdEncoding.EncodeToString([]byte("updated-value")),
		"type":        "string",
		"description": "Updated description",
		"overwrite":   true,
	}
	updateBody, _ := json.Marshal(updateReq)

	req, _ = http.NewRequest(http.MethodPut, base+"/api/v1/parameters"+path, bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var setResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &setResp))
	assert.False(t, setResp["created"].(bool), "Expected created=false for update")

	// Verify update
	req, _ = http.NewRequest(http.MethodGet, base+"/api/v1/parameters"+path, nil)
	resp, err = client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ = io.ReadAll(resp.Body)
	var getResp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &getResp))

	param := getResp["parameter"].(map[string]interface{})
	decodedValue, _ := base64.StdEncoding.DecodeString(param["value"].(string))
	assert.Equal(t, "updated-value", string(decodedValue))
	assert.Equal(t, "Updated description", param["description"])

	// Cleanup
	req, _ = http.NewRequest(http.MethodDelete, base+"/api/v1/parameters"+path, nil)
	resp, _ = client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
}

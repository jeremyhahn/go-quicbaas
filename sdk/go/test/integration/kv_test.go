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

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// entityRequest represents an entity operation request matching the REST API.
type entityRequest struct {
	EntityType string          `json:"entity_type,omitempty"`
	ID         uint64          `json:"id,omitempty"`
	Data       json.RawMessage `json:"data"`
}

// entityResponse represents an entity operation response from the REST API.
type entityResponse struct {
	ID         uint64          `json:"id"`
	EntityType string          `json:"entity_type"`
	Data       json.RawMessage `json:"data"`
	Version    uint64          `json:"version,omitempty"`
	Created    bool            `json:"created,omitempty"`
	Timestamp  time.Time       `json:"timestamp,omitempty"`
}

// listEntitiesResponse represents a list entities response.
type listEntitiesResponse struct {
	Entities []entityResponse `json:"entities,omitempty"`
	Total    int              `json:"total,omitempty"`
}

// idCounter is used to generate unique numeric IDs for tests.
var idCounter uint64 = uint64(time.Now().UnixNano() % 1000000)

// nextID returns the next unique ID for testing.
func nextID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

// TestEntityPutGet tests basic Put and Get operations against the entity API.
func TestEntityPutGet(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("put and get single entity", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID()
		data := map[string]interface{}{"value": "test_data", "name": "entity1"}

		// Put the entity
		putResp, err := entityPut(ctx, entityType, id, data)
		if err != nil {
			t.Fatalf("put failed: %v", err)
		}

		t.Logf("put response: id=%d, created=%v", putResp.ID, putResp.Created)

		// Get the entity
		getResp, err := entityGet(ctx, entityType, id)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}

		if getResp.ID != id {
			t.Errorf("expected id %d, got %d", id, getResp.ID)
		}

		// Verify data
		var gotData map[string]interface{}
		if err := json.Unmarshal(getResp.Data, &gotData); err != nil {
			t.Fatalf("failed to unmarshal data: %v", err)
		}

		if gotData["value"] != "test_data" {
			t.Errorf("expected value 'test_data', got %v", gotData["value"])
		}
	})

	t.Run("put updates existing entity", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID()
		data1 := map[string]interface{}{"version": "v1"}
		data2 := map[string]interface{}{"version": "v2"}

		// Put initial value
		_, err := entityPut(ctx, entityType, id, data1)
		if err != nil {
			t.Fatalf("first put failed: %v", err)
		}

		// Update the value
		_, err = entityPut(ctx, entityType, id, data2)
		if err != nil {
			t.Fatalf("second put failed: %v", err)
		}

		// Get should return updated value
		getResp, err := entityGet(ctx, entityType, id)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}

		var gotData map[string]interface{}
		if err := json.Unmarshal(getResp.Data, &gotData); err != nil {
			t.Fatalf("failed to unmarshal data: %v", err)
		}

		if gotData["version"] != "v2" {
			t.Errorf("expected version 'v2', got %v", gotData["version"])
		}
	})

	t.Run("get non-existent entity returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID() + 999999 // Use an ID that doesn't exist

		_, err := entityGet(ctx, entityType, id)
		if err == nil {
			t.Error("expected error for non-existent entity")
		}
		t.Logf("get non-existent returned error (expected): %v", err)
	})

	t.Run("put large data", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID()

		// Create data with a large field
		largeValue := make([]byte, 1024)
		for i := range largeValue {
			largeValue[i] = byte('A' + (i % 26))
		}
		data := map[string]interface{}{"large_field": string(largeValue)}

		_, err := entityPut(ctx, entityType, id, data)
		if err != nil {
			t.Fatalf("put large data failed: %v", err)
		}

		getResp, err := entityGet(ctx, entityType, id)
		if err != nil {
			t.Fatalf("get large data failed: %v", err)
		}

		var gotData map[string]interface{}
		if err := json.Unmarshal(getResp.Data, &gotData); err != nil {
			t.Fatalf("failed to unmarshal data: %v", err)
		}

		if gotData["large_field"] != string(largeValue) {
			t.Error("large data mismatch")
		}
	})
}

// TestEntityDelete tests Delete operations.
func TestEntityDelete(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("delete existing entity", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID()
		data := map[string]interface{}{"to_delete": true}

		// Put the entity
		_, err := entityPut(ctx, entityType, id, data)
		if err != nil {
			t.Fatalf("put failed: %v", err)
		}

		// Delete the entity
		err = entityDelete(ctx, entityType, id)
		if err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		// Verify entity is gone
		_, err = entityGet(ctx, entityType, id)
		if err == nil {
			t.Error("expected error for deleted entity")
		}
	})

	t.Run("delete non-existent entity", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "test_entities"
		id := nextID() + 999999

		// Deleting non-existent entity may succeed or fail depending on implementation
		err := entityDelete(ctx, entityType, id)
		t.Logf("delete non-existent response: error=%v", err)
	})
}

// TestEntityList tests List operations.
func TestEntityList(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("list entities", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := fmt.Sprintf("list_test_%d", time.Now().UnixNano())
		numEntities := 5

		// Create entities
		for i := 0; i < numEntities; i++ {
			id := nextID()
			data := map[string]interface{}{"index": i}
			_, err := entityPut(ctx, entityType, id, data)
			if err != nil {
				t.Fatalf("put entity %d failed: %v", i, err)
			}
		}

		// List entities
		listResp, err := entityList(ctx, entityType, 0, "")
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}

		if len(listResp.Entities) < numEntities {
			t.Errorf("expected at least %d entities, got %d", numEntities, len(listResp.Entities))
		}
	})
}

// TestEntityConcurrent tests concurrent entity operations.
func TestEntityConcurrent(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("concurrent puts to different entities", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "concurrent_test"
		numGoroutines := 10

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				id := nextID()
				data := map[string]interface{}{"goroutine": idx}
				_, err := entityPut(ctx, entityType, id, data)
				if err != nil {
					errors <- &qberrors.WrappedMessageError{
						Message: "goroutine " + strconv.Itoa(idx),
						Cause:   err,
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		var errs []error
		for err := range errors {
			errs = append(errs, err)
		}

		if len(errs) > 0 {
			t.Errorf("concurrent puts had %d errors: %v", len(errs), errs)
		}
	})

	t.Run("concurrent puts to same entity", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "concurrent_test"
		id := nextID()
		numGoroutines := 10

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				data := map[string]interface{}{"goroutine": idx}
				_, err := entityPut(ctx, entityType, id, data)
				if err != nil {
					errors <- &qberrors.WrappedMessageError{
						Message: "goroutine " + strconv.Itoa(idx),
						Cause:   err,
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		var errs []error
		for err := range errors {
			errs = append(errs, err)
		}

		if len(errs) > 0 {
			t.Errorf("concurrent puts to same entity had %d errors: %v", len(errs), errs)
		}

		// Verify entity exists with some value
		getResp, err := entityGet(ctx, entityType, id)
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}

		if getResp.ID != id {
			t.Error("expected entity to exist")
		}
	})
}

// TestEntityErrorHandling tests error handling scenarios.
func TestEntityErrorHandling(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("malformed request", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		putURL := fmt.Sprintf("%s/api/v1/entities/test/123", GetRESTAddress())

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader([]byte("not json")))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		authorize(req)
		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			t.Error("expected error for malformed request")
		}
	})
}

// Helper functions for entity operations

func entityPut(ctx context.Context, entityType string, id uint64, data interface{}) (*entityResponse, error) {
	putURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", GetRESTAddress(), entityType, id)

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to marshal data", Cause: err}
	}

	body, err := json.Marshal(entityRequest{
		Data: dataJSON,
	})
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to marshal request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(body))
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")

	authorize(req)
	client := newHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, &qberrors.MessageError{
			Message: "put failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var entityResp entityResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &entityResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &entityResp, nil
}

func entityGet(ctx context.Context, entityType string, id uint64) (*entityResponse, error) {
	getURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", GetRESTAddress(), entityType, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL, nil)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	authorize(req)
	client := newHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &qberrors.MessageError{Message: "entity not found"}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{
			Message: "get failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var entityResp entityResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &entityResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &entityResp, nil
}

func entityDelete(ctx context.Context, entityType string, id uint64) error {
	deleteURL := fmt.Sprintf("%s/api/v1/entities/%s/%d", GetRESTAddress(), entityType, id)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	authorize(req)
	client := newHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return &qberrors.MessageError{
			Message: "delete failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	return nil
}

func entityList(ctx context.Context, entityType string, limit int, cursor string) (*listEntitiesResponse, error) {
	listURL := fmt.Sprintf("%s/api/v1/entities/%s", GetRESTAddress(), entityType)

	// Build query parameters
	params := url.Values{}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	if len(params) > 0 {
		listURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to create request", Cause: err}
	}

	authorize(req)
	client := newHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &qberrors.WrappedMessageError{Message: "failed to read response", Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{
			Message: "list failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var listResp listEntitiesResponse
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &listResp); err != nil {
			return nil, &qberrors.WrappedMessageError{Message: "failed to unmarshal response", Cause: err}
		}
	}

	return &listResp, nil
}

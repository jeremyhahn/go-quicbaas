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

//go:build integration && agents

package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Sentinel errors for agent E2E integration test infrastructure.
var (
	// ErrOrchestratorUnavailable indicates the orchestrator service is not reachable.
	ErrOrchestratorUnavailable = errors.New("agents integration: orchestrator unavailable")

	// ErrOllamaUnavailable indicates the Ollama service is not reachable.
	ErrOllamaUnavailable = errors.New("agents integration: ollama unavailable")

	// ErrHTTPRequest indicates an HTTP request operation failed.
	ErrHTTPRequest = errors.New("agents integration: http request failed")

	// ErrHTTPResponse indicates an unexpected HTTP response.
	ErrHTTPResponse = errors.New("agents integration: unexpected http response")
)

// HTTPRequestError provides details about an HTTP request failure.
type HTTPRequestError struct {
	URL    string
	Method string
	Cause  error
}

// Error implements the error interface.
func (e *HTTPRequestError) Error() string {
	msg := "agents integration: http " + e.Method + " " + e.URL
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HTTPRequestError) Unwrap() error {
	return ErrHTTPRequest
}

// HTTPResponseError provides details about an unexpected HTTP response.
type HTTPResponseError struct {
	URL        string
	Method     string
	StatusCode int
	Body       string
}

// Error implements the error interface.
func (e *HTTPResponseError) Error() string {
	return "agents integration: http " + e.Method + " " + e.URL +
		" returned status " + http.StatusText(e.StatusCode) +
		": " + e.Body
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *HTTPResponseError) Unwrap() error {
	return ErrHTTPResponse
}

// newTLSClient creates an HTTP client that presents the tenant operator's
// bearer: every orchestrator route these tests call is authenticated.
func newTLSClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	return helpers.TenantOperatorHTTPClient(ctx, timeout)
}

// postJSON sends an HTTP POST request with a JSON body and returns the response.
// The caller is responsible for closing the response body.
func postJSON(ctx context.Context, url string, body interface{}) (*http.Response, error) {
	data, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "POST", Cause: marshalErr}
	}

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if reqErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "POST", Cause: reqErr}
	}
	req.Header.Set("Content-Type", "application/json")

	client, clientErr := newTLSClient(ctx, 30*time.Second)
	if clientErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "POST", Cause: clientErr}
	}
	resp, doErr := client.Do(req)
	if doErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "POST", Cause: doErr}
	}
	return resp, nil
}

// getJSON sends an HTTP GET request and decodes the JSON response body into result.
// If result is nil, the response body is not decoded. The response is always returned
// for status code inspection.
func getJSON(ctx context.Context, url string, result interface{}) (*http.Response, error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if reqErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "GET", Cause: reqErr}
	}
	req.Header.Set("Accept", "application/json")

	client, clientErr := newTLSClient(ctx, 30*time.Second)
	if clientErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "GET", Cause: clientErr}
	}
	resp, doErr := client.Do(req)
	if doErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "GET", Cause: doErr}
	}

	if result != nil {
		defer resp.Body.Close()
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return resp, &HTTPRequestError{URL: url, Method: "GET", Cause: readErr}
		}
		if decodeErr := json.Unmarshal(bodyBytes, result); decodeErr != nil {
			return resp, &HTTPRequestError{URL: url, Method: "GET", Cause: decodeErr}
		}
	}

	return resp, nil
}

// deleteJSON sends an HTTP DELETE request and returns the response.
// The caller is responsible for closing the response body.
func deleteJSON(ctx context.Context, url string) (*http.Response, error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if reqErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "DELETE", Cause: reqErr}
	}
	req.Header.Set("Accept", "application/json")

	client, clientErr := newTLSClient(ctx, 30*time.Second)
	if clientErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "DELETE", Cause: clientErr}
	}
	resp, doErr := client.Do(req)
	if doErr != nil {
		return nil, &HTTPRequestError{URL: url, Method: "DELETE", Cause: doErr}
	}
	return resp, nil
}

// requireOrchestrator fails the test immediately if the orchestrator is not available.
func requireOrchestrator(t *testing.T) {
	t.Helper()
	require.True(t, env.quicbaasAvailable,
		"QuicBaaS orchestrator must be available at %s", env.quicbaasURL)
}

// requireOllama fails the test immediately if Ollama is not available.
func requireOllama(t *testing.T) {
	t.Helper()
	require.True(t, env.ollamaAvailable,
		"Ollama service must be available at %s", env.ollamaURL)
}

// readResponseBody reads and returns the full response body, closing it afterward.
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, &HTTPRequestError{
			URL:    resp.Request.URL.String(),
			Method: resp.Request.Method,
			Cause:  readErr,
		}
	}
	return body, nil
}

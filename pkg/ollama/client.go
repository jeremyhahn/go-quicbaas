//go:build rag

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

package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

const (
	// DefaultBaseURL is the default Ollama API endpoint, as stated by the
	// configuration defaults layer.
	DefaultBaseURL = config.DefaultOllamaBaseURL

	// DefaultTimeout is the default HTTP timeout.
	DefaultTimeout = 120 * time.Second

	// DefaultMaxRetries is the default number of retries.
	DefaultMaxRetries = 3

	// DefaultRetryBackoff is the default backoff duration between retries.
	DefaultRetryBackoff = 1 * time.Second

	// DefaultHealthCheckInterval is the default interval for health checks.
	DefaultHealthCheckInterval = 30 * time.Second
)

// ClientConfig holds configuration for the Ollama client.
type ClientConfig struct {
	// BaseURL is the Ollama API endpoint.
	BaseURL string

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int

	// RetryBackoff is the base backoff duration between retries.
	RetryBackoff time.Duration

	// HealthCheckInterval is the interval for health checks.
	HealthCheckInterval time.Duration
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		BaseURL:             DefaultBaseURL,
		Timeout:             DefaultTimeout,
		MaxRetries:          DefaultMaxRetries,
		RetryBackoff:        DefaultRetryBackoff,
		HealthCheckInterval: DefaultHealthCheckInterval,
	}
}

// Client provides HTTP communication with the Ollama API.
type Client struct {
	httpClient          *http.Client
	baseURL             string
	maxRetries          int
	retryBackoff        time.Duration
	healthCheckInterval time.Duration
}

// NewClient creates a new Ollama API client.
func NewClient(cfg ClientConfig) *Client {
	// Apply defaults for zero values
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryBackoff == 0 {
		cfg.RetryBackoff = DefaultRetryBackoff
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = DefaultHealthCheckInterval
	}

	// Normalize base URL
	baseURL := strings.TrimSuffix(cfg.BaseURL, "/")

	// Create HTTP client with connection pooling
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableKeepAlives:   false,
		},
	}

	return &Client{
		httpClient:          httpClient,
		baseURL:             baseURL,
		maxRetries:          cfg.MaxRetries,
		retryBackoff:        cfg.RetryBackoff,
		healthCheckInterval: cfg.HealthCheckInterval,
	}
}

// BaseURL returns the configured base URL.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Close closes the client and releases resources.
func (c *Client) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// doRequest performs an HTTP request with retries.
func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff duration with exponential increase
			backoff := c.retryBackoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, &OllamaSentinelCauseError{Sentinel: ErrContextCanceled, Cause: ctx.Err()}
			case <-time.After(backoff):
			}
		}

		respBody, err := c.doRequestOnce(ctx, method, path, body)
		if err == nil {
			return respBody, nil
		}

		lastErr = err

		// Don't retry on client errors or context cancellation
		if errors.Is(err, ErrInvalidInput) ||
			errors.Is(err, ErrModelNotFound) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		// Retry on connection errors and server errors
		if !isConnectionRefused(err) && !errors.Is(err, ErrOllamaUnavailable) {
			return nil, err
		}
	}

	return nil, &OllamaRetryExhaustedError{MaxRetries: c.maxRetries, Cause: lastErr}
}

// doRequestOnce performs a single HTTP request without retries.
func (c *Client) doRequestOnce(ctx context.Context, method, path string, body any) ([]byte, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, &OllamaSentinelCauseError{Sentinel: ErrInvalidInput, Cause: err}
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrRequestFailed, Cause: err}
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if isConnectionRefused(err) {
			return nil, &OllamaUnavailableAtError{BaseURL: c.baseURL}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &OllamaSentinelCauseError{Sentinel: ErrTimeout, Cause: err}
		}
		return nil, &OllamaSentinelCauseError{Sentinel: ErrOllamaUnavailable, Cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &OllamaSentinelCauseError{Sentinel: ErrRequestFailed, Cause: err}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleErrorResponse(resp.StatusCode, respBody)
	}

	return respBody, nil
}

// handleErrorResponse maps HTTP error responses to appropriate errors.
func (c *Client) handleErrorResponse(statusCode int, body []byte) error {
	// Try to parse error message
	var errResp ErrorResponse
	var errMsg string
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		errMsg = errResp.Error
	} else {
		errMsg = string(body)
	}
	if errMsg == "" {
		errMsg = fmt.Sprintf("HTTP %d", statusCode)
	}

	switch statusCode {
	case http.StatusBadRequest:
		return &OllamaSentinelMessageError{Sentinel: ErrInvalidInput, Message: errMsg}
	case http.StatusNotFound:
		if strings.Contains(errMsg, "model") {
			return &OllamaSentinelMessageError{Sentinel: ErrModelNotFound, Message: errMsg}
		}
		return &OllamaSentinelMessageError{Sentinel: ErrRequestFailed, Message: errMsg}
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return &OllamaSentinelMessageError{Sentinel: ErrOllamaUnavailable, Message: errMsg}
	default:
		return &OllamaHTTPError{Sentinel: ErrRequestFailed, StatusCode: statusCode, Message: errMsg}
	}
}

// isConnectionRefused checks if the error is a connection refused error.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}

	var netErr *net.OpError
	if ok := errors.As(err, &netErr); ok {
		if errors.Is(netErr.Err, syscall.ECONNREFUSED) {
			return true
		}
	}

	return false
}

//go:build !rest

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

// Package rest provides RESTful HTTP API for QuicBaaS.
// This is a stub file compiled when the rest build tag is not set.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrRESTNotCompiled is returned when REST API functions are called without the rest build tag.
var ErrRESTNotCompiled = errors.New("rest api support not compiled - build with -tags rest")

// Config holds REST API server configuration.
// This is a stub - REST API support is not compiled in.
type Config struct {
	Address        string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxHeaderBytes int
	CORSOrigins    []string
	RequestTimeout time.Duration
	RateLimit      int
}

// DefaultConfig returns default REST API configuration.
// This is a stub - REST API support is not compiled in.
func DefaultConfig() *Config {
	return &Config{
		Address:        ":8080",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
		RequestTimeout: 30 * time.Second,
		RateLimit:      0,
	}
}

// Server represents the REST API server.
// This is a stub - REST API support is not compiled in.
type Server struct{}

// NewServer creates a new REST API server.
// This is a stub - REST API support is not compiled in.
// Build with -tags rest to enable REST API support.
func NewServer(svc service.QuicBaaS, config *Config, log *logger.Logger) (*Server, error) {
	return nil, ErrRESTNotCompiled
}

// Start starts the HTTP server.
// This is a stub - REST API support is not compiled in.
func (s *Server) Start(address string) error {
	return ErrRESTNotCompiled
}

// Shutdown gracefully shuts down the server.
// This is a stub - REST API support is not compiled in.
func (s *Server) Shutdown(ctx context.Context) error {
	return ErrRESTNotCompiled
}

// Close closes the server.
// This is a stub - REST API support is not compiled in.
func (s *Server) Close() error {
	return ErrRESTNotCompiled
}

// IsStarted returns false when REST support is not compiled.
func (s *Server) IsStarted() bool {
	return false
}

// IsClosed returns true when REST support is not compiled.
func (s *Server) IsClosed() bool {
	return true
}

// Uptime returns zero when REST support is not compiled.
func (s *Server) Uptime() time.Duration {
	return 0
}

// Addr returns nil when REST support is not compiled.
func (s *Server) Addr() net.Addr {
	return nil
}

// RESTClient implements the QuicBaaS SDK client interfaces using REST API.
// This is a stub - REST API support is not compiled in.
type RESTClient struct{}

// ClientOption configures the REST client.
// This is a stub - REST API support is not compiled in.
type ClientOption func(*RESTClient) error

// NewRESTClient creates a new REST API client.
// This is a stub - REST API support is not compiled in.
// Build with -tags rest to enable REST API support.
func NewRESTClient(baseURL string, log *logger.Logger, opts ...ClientOption) (*RESTClient, error) {
	return nil, ErrRESTNotCompiled
}

// WithTimeout sets the HTTP client timeout.
// This is a stub - REST API support is not compiled in.
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *RESTClient) error {
		return ErrRESTNotCompiled
	}
}

// WithTLS configures TLS for the REST client.
// This is a stub - REST API support is not compiled in.
func WithTLS(certFile, keyFile, caFile string) ClientOption {
	return func(c *RESTClient) error {
		return ErrRESTNotCompiled
	}
}

// Close closes the REST client.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) Close() error {
	return ErrRESTNotCompiled
}

// =============================================================================
// SchedulerClient Interface Stubs
// =============================================================================

// SchedulerCreateJob creates a new scheduled job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerCreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerGetJob retrieves a job by ID.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerGetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerUpdateJob updates an existing job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerUpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerDeleteJob removes a scheduled job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerDeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerListJobs returns jobs matching the query criteria.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerTriggerJob immediately executes a job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerTriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerPauseJob pauses a scheduled job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerPauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerResumeJob resumes a paused job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerCancelJob cancels a scheduled job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerCancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerGetJobHistory returns execution history for a job.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerGetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return nil, ErrRESTNotCompiled
}

// SchedulerGetStatus returns scheduler status and statistics.
// This is a stub - REST API support is not compiled in.
func (c *RESTClient) SchedulerGetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return nil, ErrRESTNotCompiled
}

// =============================================================================
// Transaction Types
// =============================================================================

// CompareCondition represents a single comparison in a transaction.
// This is a stub - REST API support is not compiled in.
type CompareCondition struct {
	EntityType string          `json:"entity_type"`
	ID         uint64          `json:"id"`
	Target     CompareTarget   `json:"target"`
	Operator   CompareOperator `json:"operator"`
	Value      json.RawMessage `json:"value,omitempty"`
	Version    int64           `json:"version,omitempty"`
}

// CompareTarget specifies what to compare.
type CompareTarget string

const (
	CompareTargetVersion CompareTarget = "version"
	CompareTargetValue   CompareTarget = "value"
	CompareTargetExists  CompareTarget = "exists"
)

// CompareOperator specifies the comparison operator.
type CompareOperator string

const (
	CompareOpEqual     CompareOperator = "equal"
	CompareOpNotEqual  CompareOperator = "not_equal"
	CompareOpGreater   CompareOperator = "greater"
	CompareOpLess      CompareOperator = "less"
	CompareOpGreaterEq CompareOperator = "greater_equal"
	CompareOpLessEq    CompareOperator = "less_equal"
)

// TransactionOp represents a single operation in a transaction.
// This is a stub - REST API support is not compiled in.
type TransactionOp struct {
	Type       TransactionOpType `json:"type"`
	EntityType string            `json:"entity_type"`
	ID         uint64            `json:"id"`
	Data       json.RawMessage   `json:"data,omitempty"`
}

// TransactionOpType specifies the operation type.
type TransactionOpType string

const (
	TxnOpPut    TransactionOpType = "put"
	TxnOpDelete TransactionOpType = "delete"
	TxnOpGet    TransactionOpType = "get"
)

// TransactionRequest represents a transaction with compare-and-swap semantics.
// This is a stub - REST API support is not compiled in.
type TransactionRequest struct {
	Compare []CompareCondition `json:"compare,omitempty"`
	Success []TransactionOp    `json:"success,omitempty"`
	Failure []TransactionOp    `json:"failure,omitempty"`
}

// ValidationError represents a validation error for transaction requests.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// Validate validates the transaction request.
// This validation logic works without REST server functionality.
func (r *TransactionRequest) Validate() error {
	// At least one operation (success or failure) required
	if len(r.Success) == 0 && len(r.Failure) == 0 {
		return &ValidationError{
			Message: "at least one success or failure operation required",
		}
	}

	// Validate compare conditions
	for i, cmp := range r.Compare {
		if cmp.EntityType == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("compare[%d].entity_type", i),
				Message: "entity_type is required",
			}
		}
		if cmp.ID == 0 {
			return &ValidationError{
				Field:   fmt.Sprintf("compare[%d].id", i),
				Message: "id is required",
			}
		}
		if !isValidCompareTarget(cmp.Target) {
			return &ValidationError{
				Field:   fmt.Sprintf("compare[%d].target", i),
				Message: "invalid compare target",
			}
		}
		if !isValidCompareOperator(cmp.Operator) {
			return &ValidationError{
				Field:   fmt.Sprintf("compare[%d].operator", i),
				Message: "invalid compare operator",
			}
		}
	}

	// Validate success operations
	for i, op := range r.Success {
		if err := validateTransactionOp(op, i, "success"); err != nil {
			return err
		}
	}

	// Validate failure operations
	for i, op := range r.Failure {
		if err := validateTransactionOp(op, i, "failure"); err != nil {
			return err
		}
	}

	return nil
}

func validateTransactionOp(op TransactionOp, index int, opList string) error {
	if op.EntityType == "" {
		return &ValidationError{
			Field:   fmt.Sprintf("%s[%d].entity_type", opList, index),
			Message: "entity_type is required",
		}
	}
	if op.ID == 0 {
		return &ValidationError{
			Field:   fmt.Sprintf("%s[%d].id", opList, index),
			Message: "id is required",
		}
	}
	if !isValidTransactionOpType(op.Type) {
		return &ValidationError{
			Field:   fmt.Sprintf("%s[%d].type", opList, index),
			Message: "invalid operation type",
		}
	}
	if op.Type == TxnOpPut && len(op.Data) == 0 {
		return &ValidationError{
			Field:   fmt.Sprintf("%s[%d].data", opList, index),
			Message: "data is required for put operations",
		}
	}
	return nil
}

func isValidCompareTarget(target CompareTarget) bool {
	switch target {
	case CompareTargetVersion, CompareTargetValue, CompareTargetExists:
		return true
	default:
		return false
	}
}

func isValidCompareOperator(op CompareOperator) bool {
	switch op {
	case CompareOpEqual, CompareOpNotEqual, CompareOpGreater, CompareOpLess, CompareOpGreaterEq, CompareOpLessEq:
		return true
	default:
		return false
	}
}

func isValidTransactionOpType(opType TransactionOpType) bool {
	switch opType {
	case TxnOpPut, TxnOpDelete, TxnOpGet:
		return true
	default:
		return false
	}
}

// TransactionResponse represents the result of a transaction.
// This is a stub - REST API support is not compiled in.
type TransactionResponse struct {
	Succeeded bool                    `json:"succeeded"`
	Responses []TransactionOpResponse `json:"responses,omitempty"`
	Timestamp time.Time               `json:"timestamp"`
	Message   string                  `json:"message,omitempty"`
}

// TransactionOpResponse represents the result of a single operation.
// This is a stub - REST API support is not compiled in.
type TransactionOpResponse struct {
	Type       TransactionOpType `json:"type"`
	EntityType string            `json:"entity_type"`
	ID         uint64            `json:"id"`
	Data       json.RawMessage   `json:"data,omitempty"`
	Success    bool              `json:"success"`
	Error      string            `json:"error,omitempty"`
}

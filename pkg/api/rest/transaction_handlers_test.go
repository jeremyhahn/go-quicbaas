//go:build rest

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

package rest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandleTransaction(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "successful transaction",
			requestBody: TransactionRequest{
				Compare: []CompareCondition{
					{
						EntityType: "users",
						ID:         1,
						Target:     CompareTargetVersion,
						Operator:   CompareOpEqual,
						Version:    5,
					},
				},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "users",
						ID:         1,
						Data:       json.RawMessage(`{"name":"Updated"}`),
					},
				},
				Failure: []TransactionOp{},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Transaction", mock.Anything, mock.Anything).Return(&service.TransactionResponse{
					Succeeded: true,
					Responses: []*service.TransactionOpResponse{
						{
							OpType:     service.TransactionOpTypePut,
							EntityType: "users",
							ID:         1,
							Data:       []byte(`{"name":"Updated"}`),
							Success:    true,
						},
					},
					Timestamp: 123456789,
					Message:   "Transaction succeeded",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp TransactionResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.True(t, resp.Succeeded)
				assert.Len(t, resp.Responses, 1)
				assert.Equal(t, "Transaction succeeded", resp.Message)
			},
		},
		{
			name: "failed comparison in transaction",
			requestBody: TransactionRequest{
				Compare: []CompareCondition{
					{
						EntityType: "users",
						ID:         1,
						Target:     CompareTargetVersion,
						Operator:   CompareOpEqual,
						Version:    999,
					},
				},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "users",
						ID:         1,
						Data:       json.RawMessage(`{"name":"Updated"}`),
					},
				},
				Failure: []TransactionOp{
					{
						Type:       TxnOpGet,
						EntityType: "users",
						ID:         1,
					},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Transaction", mock.Anything, mock.Anything).Return(&service.TransactionResponse{
					Succeeded: false,
					Responses: []*service.TransactionOpResponse{
						{
							OpType:     service.TransactionOpTypeGet,
							EntityType: "users",
							ID:         1,
							Data:       []byte(`{"name":"Current"}`),
							Success:    true,
						},
					},
					Timestamp: 123456789,
					Message:   "Comparison failed",
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp TransactionResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				require.NoError(t, err)
				assert.False(t, resp.Succeeded)
				assert.Equal(t, "Comparison failed", resp.Message)
			},
		},
		{
			name:           "invalid json body",
			requestBody:    "invalid json",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid JSON")
			},
		},
		{
			name: "validation error - empty entity type",
			requestBody: TransactionRequest{
				Compare: []CompareCondition{},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "", // Invalid: empty
						ID:         1,
						Data:       json.RawMessage(`{}`),
					},
				},
			},
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "entity_type")
			},
		},
		{
			name: "service error",
			requestBody: TransactionRequest{
				Compare: []CompareCondition{},
				Success: []TransactionOp{
					{
						Type:       TxnOpPut,
						EntityType: "users",
						ID:         1,
						Data:       json.RawMessage(`{}`),
					},
				},
			},
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Transaction", mock.Anything, mock.Anything).Return(
					nil,
					&errors.MessageError{Message: "service error"},
				).Once()
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				// Just verify we got an error response
				assert.NotEmpty(t, w.Body.String())
			},
		},
		{
			name: "negative compare version is invalid input",
			requestBody: TransactionRequest{
				Compare: []CompareCondition{
					{
						EntityType: "users",
						ID:         1,
						Target:     CompareTargetVersion,
						Operator:   CompareOpEqual,
						Version:    -1,
					},
				},
				Success: []TransactionOp{
					{Type: TxnOpGet, EntityType: "users", ID: 1},
				},
			},
			// No Transaction expectation: the mock panics if the
			// refused request reaches the service.
			setupMock:      func(*testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				requireInvalidField(t, w, "compare version")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test server
			log, err := logger.New(logger.LogConfig{
				Level:  "error",
				Format: "json",
				Output: "stdout",
			})
			require.NoError(t, err)

			mockService := testutil.NewMockQuicBaaS()
			tt.setupMock(mockService)

			serverConfig := DefaultConfig()
			serverConfig.Address = ":0"

			server, err := NewServer(mockService, serverConfig, log)
			require.NoError(t, err)
			defer server.Close()

			// Prepare request body
			var body []byte
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/api/v1/transaction", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()

			// Execute handler
			server.HandleTransaction(w, req)

			// Verify response
			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}

			// Verify all mocks were called
			mockService.AssertExpectations(t)
		})
	}
}

func TestConvertCompareTarget(t *testing.T) {
	tests := []struct {
		name     string
		input    CompareTarget
		expected service.CompareTarget
	}{
		{
			name:     "version target",
			input:    CompareTargetVersion,
			expected: service.CompareTargetVersion,
		},
		{
			name:     "value target",
			input:    CompareTargetValue,
			expected: service.CompareTargetValue,
		},
		{
			name:     "exists target",
			input:    CompareTargetExists,
			expected: service.CompareTargetExists,
		},
		{
			name:     "unspecified target",
			input:    CompareTarget("unknown"),
			expected: service.CompareTargetUnspecified,
		},
		{
			name:     "empty target",
			input:    CompareTarget(""),
			expected: service.CompareTargetUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertCompareTarget(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertCompareOperator(t *testing.T) {
	tests := []struct {
		name     string
		input    CompareOperator
		expected service.CompareOperator
	}{
		{
			name:     "equal operator",
			input:    CompareOpEqual,
			expected: service.CompareOperatorEqual,
		},
		{
			name:     "not equal operator",
			input:    CompareOpNotEqual,
			expected: service.CompareOperatorNotEqual,
		},
		{
			name:     "greater operator",
			input:    CompareOpGreater,
			expected: service.CompareOperatorGreater,
		},
		{
			name:     "less operator",
			input:    CompareOpLess,
			expected: service.CompareOperatorLess,
		},
		{
			name:     "greater or equal operator",
			input:    CompareOpGreaterEq,
			expected: service.CompareOperatorGreaterEqual,
		},
		{
			name:     "less or equal operator",
			input:    CompareOpLessEq,
			expected: service.CompareOperatorLessEqual,
		},
		{
			name:     "unknown operator",
			input:    CompareOperator("unknown"),
			expected: service.CompareOperatorUnspecified,
		},
		{
			name:     "empty operator",
			input:    CompareOperator(""),
			expected: service.CompareOperatorUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertCompareOperator(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertTxnOpType(t *testing.T) {
	tests := []struct {
		name     string
		input    TransactionOpType
		expected service.TransactionOpType
	}{
		{
			name:     "put operation",
			input:    TxnOpPut,
			expected: service.TransactionOpTypePut,
		},
		{
			name:     "get operation",
			input:    TxnOpGet,
			expected: service.TransactionOpTypeGet,
		},
		{
			name:     "delete operation",
			input:    TxnOpDelete,
			expected: service.TransactionOpTypeDelete,
		},
		{
			name:     "unknown operation",
			input:    TransactionOpType("unknown"),
			expected: service.TransactionOpTypeUnspecified,
		},
		{
			name:     "empty operation",
			input:    TransactionOpType(""),
			expected: service.TransactionOpTypeUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTxnOpType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertServiceOpTypeToREST(t *testing.T) {
	tests := []struct {
		name     string
		input    service.TransactionOpType
		expected TransactionOpType
	}{
		{
			name:     "put operation",
			input:    service.TransactionOpTypePut,
			expected: TxnOpPut,
		},
		{
			name:     "get operation",
			input:    service.TransactionOpTypeGet,
			expected: TxnOpGet,
		},
		{
			name:     "delete operation",
			input:    service.TransactionOpTypeDelete,
			expected: TxnOpDelete,
		},
		{
			name:     "unknown operation",
			input:    service.TransactionOpTypeUnspecified,
			expected: TransactionOpType(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceOpTypeToREST(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertCompareConditions(t *testing.T) {
	tests := []struct {
		name     string
		input    []CompareCondition
		validate func(*testing.T, []*service.CompareCondition)
	}{
		{
			name:  "empty conditions",
			input: []CompareCondition{},
			validate: func(t *testing.T, result []*service.CompareCondition) {
				assert.Empty(t, result)
			},
		},
		{
			name: "single condition",
			input: []CompareCondition{
				{
					EntityType: "users",
					ID:         123,
					Target:     CompareTargetVersion,
					Operator:   CompareOpEqual,
					Version:    5,
					Value:      nil,
				},
			},
			validate: func(t *testing.T, result []*service.CompareCondition) {
				assert.Len(t, result, 1)
				assert.Equal(t, "users", result[0].EntityType)
				assert.Equal(t, uint64(123), result[0].ID)
				assert.Equal(t, service.CompareTargetVersion, result[0].Target)
				assert.Equal(t, service.CompareOperatorEqual, result[0].Operator)
				assert.Equal(t, uint64(5), result[0].Version)
			},
		},
		{
			name: "multiple conditions",
			input: []CompareCondition{
				{
					EntityType: "users",
					ID:         1,
					Target:     CompareTargetVersion,
					Operator:   CompareOpEqual,
					Version:    1,
				},
				{
					EntityType: "posts",
					ID:         2,
					Target:     CompareTargetExists,
					Operator:   CompareOpNotEqual,
					Version:    0,
				},
			},
			validate: func(t *testing.T, result []*service.CompareCondition) {
				assert.Len(t, result, 2)
				assert.Equal(t, "users", result[0].EntityType)
				assert.Equal(t, "posts", result[1].EntityType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertCompareConditions(tt.input)
			require.NoError(t, err)
			tt.validate(t, result)
		})
	}
}

func TestConvertTransactionOps(t *testing.T) {
	tests := []struct {
		name     string
		input    []TransactionOp
		validate func(*testing.T, []*service.TransactionOp)
	}{
		{
			name:  "empty operations",
			input: []TransactionOp{},
			validate: func(t *testing.T, result []*service.TransactionOp) {
				assert.Empty(t, result)
			},
		},
		{
			name: "single operation",
			input: []TransactionOp{
				{
					Type:       TxnOpPut,
					EntityType: "users",
					ID:         123,
					Data:       []byte(`{"name":"Alice"}`),
				},
			},
			validate: func(t *testing.T, result []*service.TransactionOp) {
				assert.Len(t, result, 1)
				assert.Equal(t, service.TransactionOpTypePut, result[0].OpType)
				assert.Equal(t, "users", result[0].EntityType)
				assert.Equal(t, uint64(123), result[0].ID)
				assert.Equal(t, []byte(`{"name":"Alice"}`), result[0].Data)
			},
		},
		{
			name: "multiple operations",
			input: []TransactionOp{
				{
					Type:       TxnOpPut,
					EntityType: "users",
					ID:         1,
					Data:       []byte(`{"name":"Alice"}`),
				},
				{
					Type:       TxnOpGet,
					EntityType: "posts",
					ID:         2,
				},
				{
					Type:       TxnOpDelete,
					EntityType: "comments",
					ID:         3,
				},
			},
			validate: func(t *testing.T, result []*service.TransactionOp) {
				assert.Len(t, result, 3)
				assert.Equal(t, service.TransactionOpTypePut, result[0].OpType)
				assert.Equal(t, service.TransactionOpTypeGet, result[1].OpType)
				assert.Equal(t, service.TransactionOpTypeDelete, result[2].OpType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTransactionOps(tt.input)
			tt.validate(t, result)
		})
	}
}

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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/query"
)

type mockKVClient struct {
	PutFunc    func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error)
	GetFunc    func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error)
	DeleteFunc func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error)
	ListFunc   func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error)
}

func (m *mockKVClient) Put(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
	if m.PutFunc != nil {
		return m.PutFunc(ctx, req)
	}
	return &service.PutResponse{ID: req.ID, Version: 1, Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockKVClient) Get(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, req)
	}
	return &service.GetResponse{ID: req.ID, Found: true, Data: []byte("value"), Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockKVClient) Delete(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, req)
	}
	return &service.DeleteResponse{Deleted: true, Timestamp: time.Now().UnixNano()}, nil
}

func (m *mockKVClient) List(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, req)
	}
	return &service.ListResponse{Entities: []*service.Entity{}, TotalCount: 0}, nil
}

func (m *mockKVClient) Watch(ctx context.Context, key string, opts ...kv.WatchOption) (kv.Watcher, error) {
	return nil, nil
}

func (m *mockKVClient) WatchPrefix(ctx context.Context, prefix string, opts ...kv.WatchOption) (kv.Watcher, error) {
	return nil, nil
}

// mockQueryClient implements query.Client for testing.
type mockQueryClient struct {
	ExecuteFunc func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error)
}

func (m *mockQueryClient) Execute(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, req)
	}
	return &query.QueryResponse{Results: []query.QueryResult{}, HasMore: false}, nil
}

// mockSDKClient implements quicbaas.Client for testing.
type mockSDKClient struct {
	kvClient    *mockKVClient
	queryClient *mockQueryClient
	closeFunc   func() error
}

func (m *mockSDKClient) KV() quicbaas.KVClient {
	if m.kvClient != nil {
		return m.kvClient
	}
	return &mockKVClient{}
}

func (m *mockSDKClient) Query() quicbaas.QueryClient {
	if m.queryClient != nil {
		return m.queryClient
	}
	return &mockQueryClient{}
}

func (m *mockSDKClient) Batch() quicbaas.BatchClient                           { return nil }
func (m *mockSDKClient) Transaction() quicbaas.TransactionClient               { return nil }
func (m *mockSDKClient) Cluster() quicbaas.ClusterClient                       { return nil }
func (m *mockSDKClient) Health() quicbaas.HealthClient                         { return nil }
func (m *mockSDKClient) Operations() quicbaas.OperationsClient                 { return nil }
func (m *mockSDKClient) Cache() quicbaas.CacheClient                           { return nil }
func (m *mockSDKClient) IAM() quicbaas.IAMClient                               { return nil }
func (m *mockSDKClient) Vector() quicbaas.VectorClient                         { return nil }
func (m *mockSDKClient) TimeSeries() quicbaas.TimeSeriesClient                 { return nil }
func (m *mockSDKClient) Backup() quicbaas.BackupClient                         { return nil }
func (m *mockSDKClient) ObjectStorage() quicbaas.ObjectStorageClient           { return nil }
func (m *mockSDKClient) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }
func (m *mockSDKClient) ObjectTransfer() quicbaas.ObjectTransferClient         { return nil }
func (m *mockSDKClient) ParameterStore() quicbaas.ParameterStoreClient         { return nil }
func (m *mockSDKClient) Scheduler() quicbaas.SchedulerClient                   { return nil }
func (m *mockSDKClient) Embedding() quicbaas.EmbeddingClient                   { return nil }
func (m *mockSDKClient) Ollama() quicbaas.OllamaClient                         { return nil }
func (m *mockSDKClient) Messaging() quicbaas.MessagingClient                   { return nil }
func (m *mockSDKClient) Transport() quicbaas.Transport                         { return nil }
func (m *mockSDKClient) RAG() quicbaas.RAGClient                               { return nil }
func (m *mockSDKClient) CoreData() quicbaas.CoreDataService                    { return nil }
func (m *mockSDKClient) ClusterMgmt() quicbaas.ClusterManagementService        { return nil }
func (m *mockSDKClient) Security() quicbaas.SecurityService                    { return nil }
func (m *mockSDKClient) Identity() quicbaas.IdentityService                    { return nil }
func (m *mockSDKClient) Search() quicbaas.SearchService                        { return nil }
func (m *mockSDKClient) Storage() quicbaas.StorageService                      { return nil }
func (m *mockSDKClient) Async() quicbaas.AsyncService                          { return nil }
func (m *mockSDKClient) AI() quicbaas.AIService                                { return nil }
func (m *mockSDKClient) DataStore() (quicbaas.QRDBKVStore, error)              { return nil, nil }

func (m *mockSDKClient) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService       { return nil }
func (m *mockSDKClient) TenantMgmtService() quicbaas.TenantService                  { return nil }
func (m *mockSDKClient) Barrier() (quicbaas.QRDBBarrierClient, error)               { return nil, nil }
func (m *mockSDKClient) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error)      { return nil, nil }
func (m *mockSDKClient) SealStore() (quicbaas.QRDBSealStoreClient, error)           { return nil, nil }
func (m *mockSDKClient) TenantMgmt() (quicbaas.QRDBTenantClient, error)             { return nil, nil }
func (m *mockSDKClient) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) { return nil, nil }
func (m *mockSDKClient) Share() (quicbaas.QRDBShareClient, error)                   { return nil, nil }

func (m *mockSDKClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockSDKClient) Project() quicbaas.ProjectClient { return nil }
func (m *mockSDKClient) SDLC() quicbaas.SDLCClient       { return nil }
func (m *mockSDKClient) Prompts() quicbaas.PromptsClient { return nil }
func (m *mockSDKClient) Agents() quicbaas.AgentsClient   { return nil }
func (m *mockSDKClient) Account() quicbaas.AccountClient { return nil }

func (m *mockSDKClient) Orchestrator() quicbaas.OrchestratorClient { return nil }

func TestNewDataCommands(t *testing.T) {
	cmd := newDataCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "data", cmd.Use)
	assert.Contains(t, cmd.Short, "Data manipulation")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "put", "put command should be registered")
	assert.Contains(t, commandNames, "get", "get command should be registered")
	assert.Contains(t, commandNames, "delete", "delete command should be registered")
	assert.Contains(t, commandNames, "list", "list command should be registered")
	assert.Contains(t, commandNames, "query", "query command should be registered")
}

func TestDataCommandFlags(t *testing.T) {
	cmd := newDataCommands()

	// Test that persistent flags are available
	flags := cmd.PersistentFlags()

	// Note: --address flag is now global (defined in main.go on rootCmd)
	// and is not available on the data command's local persistent flags

	// --timeout is global too (#246). The group used to re-register it bound
	// to a SECOND variable, which is what made the operator's value vanish on
	// every verb outside the seven groups that did so.
	assert.Nil(t, flags.Lookup("timeout"),
		"the data group must not shadow the root --timeout")
	require.NotNil(t, rootCmd.PersistentFlags().Lookup("timeout"),
		"timeout flag should be on root command")

	outputFlag := flags.Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	consistencyFlag := flags.Lookup("consistency")
	require.NotNil(t, consistencyFlag)
	assert.Equal(t, "linearizable", consistencyFlag.DefValue)

	// TLS flags are now registered on rootCmd as persistent flags (see main.go),
	// inherited by all subcommands. Verify they exist on rootCmd.
	tlsFlag := rootCmd.PersistentFlags().Lookup("tls")
	require.NotNil(t, tlsFlag, "tls flag should be on root command")

	maxRetriesFlag := flags.Lookup("max-retries")
	require.NotNil(t, maxRetriesFlag)
	assert.Equal(t, "3", maxRetriesFlag.DefValue)
}

func TestPutCommand(t *testing.T) {
	cmd := newPutCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "put <type> <key> <value>", cmd.Use)
	assert.Contains(t, cmd.Short, "Store")

	// Verify json flag exists
	jsonFlag := cmd.Flags().Lookup("json")
	require.NotNil(t, jsonFlag)
}

func TestPutCommandHelp(t *testing.T) {
	cmd := newPutCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "put")
	assert.Contains(t, output, "Store")
	assert.Contains(t, output, "--json")
}

func TestGetCommand(t *testing.T) {
	cmd := newGetCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "get <type> <key>", cmd.Use)
	assert.Contains(t, cmd.Short, "Retrieve")
}

func TestGetCommandHelp(t *testing.T) {
	cmd := newGetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "get")
	assert.Contains(t, output, "Retrieve")
}

func TestDeleteCommand(t *testing.T) {
	cmd := newDeleteCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "delete <type> <key>", cmd.Use)
	assert.Contains(t, cmd.Short, "Delete")

	// Verify yes flag exists
	yesFlag := cmd.Flags().Lookup("yes")
	require.NotNil(t, yesFlag)
}

func TestDeleteCommandHelp(t *testing.T) {
	cmd := newDeleteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "delete")
	assert.Contains(t, output, "Delete")
	assert.Contains(t, output, "--yes")
}

func TestListCommand(t *testing.T) {
	cmd := newListCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "list <type>", cmd.Use)
	assert.Contains(t, cmd.Short, "List")

	// Verify flags exist
	limitFlag := cmd.Flags().Lookup("limit")
	require.NotNil(t, limitFlag)

	cursorFlag := cmd.Flags().Lookup("cursor")
	require.NotNil(t, cursorFlag)
}

func TestListCommandHelp(t *testing.T) {
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "list")
	assert.Contains(t, output, "List")
	assert.Contains(t, output, "--limit")
	assert.Contains(t, output, "--cursor")
}

func TestQueryCommand(t *testing.T) {
	cmd := newQueryCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "query <type>", cmd.Use)
	assert.Contains(t, cmd.Short, "Query")

	// Verify flags exist
	indexFlag := cmd.Flags().Lookup("index")
	require.NotNil(t, indexFlag)

	valueFlag := cmd.Flags().Lookup("value")
	require.NotNil(t, valueFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	require.NotNil(t, limitFlag)
}

func TestQueryCommandHelp(t *testing.T) {
	cmd := newQueryCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "query")
	assert.Contains(t, output, "Query")
	assert.Contains(t, output, "--index")
	assert.Contains(t, output, "--value")
	assert.Contains(t, output, "--limit")
}

func TestParseServiceConsistency(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    service.ConsistencyLevel
		expectError bool
		description string
	}{
		{
			name:        "strong consistency",
			input:       "strong",
			expected:    service.ConsistencyLevelStrong,
			expectError: false,
			description: "Should parse 'strong' correctly",
		},
		{
			name:        "linearizable consistency",
			input:       "linearizable",
			expected:    service.ConsistencyLevelLinearizable,
			expectError: false,
			description: "Should parse 'linearizable' correctly",
		},
		{
			name:        "eventual consistency",
			input:       "eventual",
			expected:    service.ConsistencyLevelEventual,
			expectError: false,
			description: "Should parse 'eventual' correctly",
		},
		{
			name:        "stale alias",
			input:       "stale",
			expected:    service.ConsistencyLevelEventual,
			expectError: false,
			description: "Should parse 'stale' as eventual",
		},
		{
			name:        "local alias",
			input:       "local",
			expected:    service.ConsistencyLevelEventual,
			expectError: false,
			description: "Should parse 'local' as eventual",
		},
		{
			name:        "STRONG uppercase",
			input:       "STRONG",
			expected:    service.ConsistencyLevelStrong,
			expectError: false,
			description: "Should parse 'STRONG' correctly",
		},
		{
			name:        "LINEARIZABLE uppercase",
			input:       "LINEARIZABLE",
			expected:    service.ConsistencyLevelLinearizable,
			expectError: false,
			description: "Should parse 'LINEARIZABLE' correctly",
		},
		{
			name:        "invalid consistency",
			input:       "invalid",
			expectError: true,
			description: "Should reject invalid consistency level",
		},
		{
			name:        "empty string",
			input:       "",
			expected:    service.ConsistencyLevelLinearizable,
			expectError: false,
			description: "Empty string defaults to linearizable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := parseServiceConsistency(tt.input)

			if tt.expectError {
				assert.Error(t, err, tt.description)
				assert.Contains(t, err.Error(), "unknown consistency level")
			} else {
				assert.NoError(t, err, tt.description)
				assert.Equal(t, tt.expected, level)
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "simple error",
			err:  assert.AnError,
		},
		{
			name: "nil error",
			err:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// formatError returns the same error, just formats it to stderr.
			// The nil case must be a pure pass-through: no "Error: <nil>"
			// banner may be written (success paths return
			// formatError(client.X(...)) directly).
			stderr := captureStderr(t, func() {
				result := formatError(tt.err)
				if tt.err == nil {
					assert.Nil(t, result)
				} else {
					assert.Equal(t, tt.err, result)
				}
			})
			if tt.err == nil {
				assert.Empty(t, stderr, "nil error must not print an error banner")
			} else {
				assert.Contains(t, stderr, "Error", "non-nil error must be formatted to stderr")
			}
		})
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	return <-done
}

func TestPutCommandInvalidArgs(t *testing.T) {
	cmd := newPutCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Test with no arguments - should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Should fail with no arguments")
}

func TestGetCommandInvalidArgs(t *testing.T) {
	cmd := newGetCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Test with no arguments - should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Should fail with no arguments")
}

func TestDeleteCommandInvalidArgs(t *testing.T) {
	cmd := newDeleteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Test with no arguments - should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Should fail with no arguments")
}

func TestListCommandInvalidArgs(t *testing.T) {
	cmd := newListCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Test with no arguments - should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Should fail with no arguments")
}

func TestQueryCommandInvalidArgs(t *testing.T) {
	cmd := newQueryCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Test with no arguments - should fail
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Should fail with no arguments")
}

// TestRunPutCommand tests the put command execution with various scenarios.
func TestRunPutCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		flags         map[string]interface{}
		mockSetup     func(*mockKVClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful put with string value",
			args: []string{"user", "john", `{"name":"John Doe"}`},
			flags: map[string]interface{}{
				"output":      "table",
				"consistency": "linearizable",
			},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					assert.Equal(t, "user", req.EntityType)
					assert.Equal(t, `{"name":"John Doe"}`, string(req.Data))
					assert.Equal(t, service.ConsistencyLevelLinearizable, req.Consistency)
					return &service.PutResponse{
						ID:        req.ID,
						Version:   1,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful put with JSON validation",
			args: []string{"user", "john", `{"name":"John Doe"}`},
			flags: map[string]interface{}{
				"json":        true,
				"output":      "json",
				"consistency": "strong",
			},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					assert.Equal(t, service.ConsistencyLevelStrong, req.Consistency)
					return &service.PutResponse{
						ID:        req.ID,
						Version:   1,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful put with YAML output",
			args: []string{"user", "john", "value"},
			flags: map[string]interface{}{
				"output":      "yaml",
				"consistency": "eventual",
			},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					assert.Equal(t, service.ConsistencyLevelEventual, req.Consistency)
					return &service.PutResponse{
						ID:        req.ID,
						Version:   1,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful put with raw output",
			args: []string{"user", "john", "value"},
			flags: map[string]interface{}{
				"output": "raw",
			},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return &service.PutResponse{
						ID:        req.ID,
						Version:   1,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "invalid JSON input",
			args: []string{"user", "john", `{invalid json}`},
			flags: map[string]interface{}{
				"json": true,
			},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "invalid JSON",
		},
		{
			name: "factory error",
			args: []string{"user", "john", "value"},
			mockSetup: func(m *mockKVClient) {
				// Mock will not be used since factory returns error
			},
			expectError:   true,
			errorContains: "connection refused",
		},
		{
			name: "client put error",
			args: []string{"user", "john", "value"},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "put", "storage error")
				}
			},
			expectError:   true,
			errorContains: "storage error",
		},
		{
			name: "invalid output format",
			args: []string{"user", "john", "value"},
			flags: map[string]interface{}{
				"output": "invalid",
			},
			mockSetup: func(m *mockKVClient) {
				m.PutFunc = func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return &service.PutResponse{
						ID:        req.ID,
						Version:   1,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError:   true,
			errorContains: "unknown output format",
		},
		{
			name: "invalid consistency level",
			args: []string{"user", "john", "value"},
			flags: map[string]interface{}{
				"consistency": "invalid",
			},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "unknown consistency level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original factory
			origFactory := sdkClientFactory
			defer func() { sdkClientFactory = origFactory }()

			// Reset the SDK client singleton for each test
			resetSDKClient()

			if tt.name == "factory error" {
				// Mock factory to return error
				sdkClientFactory = func() (quicbaas.Client, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "factory", "connection refused")
				}
			} else {
				// Create mock client
				mockKV := &mockKVClient{}
				tt.mockSetup(mockKV)

				mockClient := &mockSDKClient{
					kvClient: mockKV,
				}

				sdkClientFactory = func() (quicbaas.Client, error) {
					return mockClient, nil
				}
			}

			// Set flags
			if tt.flags != nil {
				for key, value := range tt.flags {
					switch key {
					case "json":
						jsonInput = value.(bool)
					case "output":
						outputFormat = value.(string)
					case "consistency":
						consistency = value.(string)
					}
				}
			}

			// Reset flags after test
			defer func() {
				jsonInput = false
				outputFormat = "table"
				consistency = "linearizable"
			}()

			cmd := &cobra.Command{}
			err := runPutCommand(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunGetCommand tests the get command execution with various scenarios.
func TestRunGetCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		flags         map[string]interface{}
		mockSetup     func(*mockKVClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful get with table output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"output":      "table",
				"consistency": "linearizable",
			},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					assert.Equal(t, "user", req.EntityType)
					assert.Equal(t, service.ConsistencyLevelLinearizable, req.Consistency)
					return &service.GetResponse{
						ID:        req.ID,
						Data:      []byte(`{"name":"John Doe"}`),
						Found:     true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful get with JSON output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"output":      "json",
				"consistency": "strong",
			},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					assert.Equal(t, service.ConsistencyLevelStrong, req.Consistency)
					return &service.GetResponse{
						ID:        req.ID,
						Data:      []byte(`{"name":"John Doe"}`),
						Found:     true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful get with YAML output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"output":      "yaml",
				"consistency": "eventual",
			},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					assert.Equal(t, service.ConsistencyLevelEventual, req.Consistency)
					return &service.GetResponse{
						ID:        req.ID,
						Data:      []byte(`{"name":"John Doe"}`),
						Found:     true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful get with raw output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"output": "raw",
			},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{
						ID:        req.ID,
						Data:      []byte(`{"name":"John Doe"}`),
						Found:     true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "key not found",
			args: []string{"user", "notfound"},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{
						ID:    req.ID,
						Found: false,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "factory error",
			args: []string{"user", "john"},
			mockSetup: func(m *mockKVClient) {
				// Mock will not be used
			},
			expectError:   true,
			errorContains: "connection refused",
		},
		{
			name: "client get error",
			args: []string{"user", "john"},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "get", "storage error")
				}
			},
			expectError:   true,
			errorContains: "storage error",
		},
		{
			name: "invalid output format",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"output": "invalid",
			},
			mockSetup: func(m *mockKVClient) {
				m.GetFunc = func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
					return &service.GetResponse{
						ID:        req.ID,
						Data:      []byte("value"),
						Found:     true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError:   true,
			errorContains: "unknown output format",
		},
		{
			name: "invalid consistency level",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"consistency": "invalid",
			},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "unknown consistency level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := sdkClientFactory
			defer func() { sdkClientFactory = origFactory }()

			// Reset the SDK client singleton for each test
			resetSDKClient()

			if tt.name == "factory error" {
				sdkClientFactory = func() (quicbaas.Client, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "factory", "connection refused")
				}
			} else {
				mockKV := &mockKVClient{}
				tt.mockSetup(mockKV)

				mockClient := &mockSDKClient{
					kvClient: mockKV,
				}

				sdkClientFactory = func() (quicbaas.Client, error) {
					return mockClient, nil
				}
			}

			// Set flags
			if tt.flags != nil {
				for key, value := range tt.flags {
					switch key {
					case "output":
						outputFormat = value.(string)
					case "consistency":
						consistency = value.(string)
					}
				}
			}

			defer func() {
				outputFormat = "table"
				consistency = "linearizable"
			}()

			cmd := &cobra.Command{}
			err := runGetCommand(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunDeleteCommand tests the delete command execution with various scenarios.
func TestRunDeleteCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		flags         map[string]interface{}
		mockSetup     func(*mockKVClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful delete with force flag",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":         true,
				"output":      "table",
				"consistency": "linearizable",
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					assert.Equal(t, "user", req.EntityType)
					assert.Equal(t, service.ConsistencyLevelLinearizable, req.Consistency)
					return &service.DeleteResponse{
						Deleted:   true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful delete with JSON output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":         true,
				"output":      "json",
				"consistency": "strong",
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					assert.Equal(t, service.ConsistencyLevelStrong, req.Consistency)
					return &service.DeleteResponse{
						Deleted:   true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful delete with YAML output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":         true,
				"output":      "yaml",
				"consistency": "eventual",
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					assert.Equal(t, service.ConsistencyLevelEventual, req.Consistency)
					return &service.DeleteResponse{
						Deleted:   true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful delete with raw output",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":    true,
				"output": "raw",
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return &service.DeleteResponse{
						Deleted:   true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "factory error with force flag",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes": true,
			},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "connection refused",
		},
		{
			name: "client delete error",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes": true,
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "delete", "storage error")
				}
			},
			expectError:   true,
			errorContains: "storage error",
		},
		{
			name: "invalid output format",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":    true,
				"output": "invalid",
			},
			mockSetup: func(m *mockKVClient) {
				m.DeleteFunc = func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
					return &service.DeleteResponse{
						Deleted:   true,
						Timestamp: time.Now().UnixNano(),
					}, nil
				}
			},
			expectError:   true,
			errorContains: "unknown output format",
		},
		{
			name: "invalid consistency level",
			args: []string{"user", "john"},
			flags: map[string]interface{}{
				"yes":         true,
				"consistency": "invalid",
			},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "unknown consistency level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := sdkClientFactory
			defer func() { sdkClientFactory = origFactory }()

			// Reset the SDK client singleton for each test
			resetSDKClient()

			if tt.name == "factory error with force flag" {
				sdkClientFactory = func() (quicbaas.Client, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "factory", "connection refused")
				}
			} else {
				mockKV := &mockKVClient{}
				tt.mockSetup(mockKV)

				mockClient := &mockSDKClient{
					kvClient: mockKV,
				}

				sdkClientFactory = func() (quicbaas.Client, error) {
					return mockClient, nil
				}
			}

			// Set flags
			if tt.flags != nil {
				for key, value := range tt.flags {
					switch key {
					case "yes":
						deleteForce = value.(bool)
					case "output":
						outputFormat = value.(string)
					case "consistency":
						consistency = value.(string)
					}
				}
			}

			defer func() {
				deleteForce = false
				outputFormat = "table"
				consistency = "linearizable"
			}()

			cmd := &cobra.Command{}
			err := runDeleteCommand(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestDeleteCommandCancellation tests delete cancellation without force flag.
func TestDeleteCommandCancellation(t *testing.T) {
	// Save original stdin
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	// Simulate user typing "n" (cancel)
	go func() {
		w.Write([]byte("n\n"))
		w.Close()
	}()

	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	mockKV := &mockKVClient{
		DeleteFunc: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			t.Fatal("Delete should not be called when user cancels")
			return nil, nil
		},
	}

	mockClient := &mockSDKClient{
		kvClient: mockKV,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	deleteForce = false
	defer func() { deleteForce = false }()

	cmd := &cobra.Command{}
	err = runDeleteCommand(cmd, []string{"user", "john"})
	// Should not error on cancellation
	assert.NoError(t, err)
}

// TestDeleteCommandConfirmation tests delete confirmation with "yes".
func TestDeleteCommandConfirmation(t *testing.T) {
	// Save original stdin
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	// Simulate user typing "yes"
	go func() {
		w.Write([]byte("yes\n"))
		w.Close()
	}()

	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	deleteCalled := false
	mockKV := &mockKVClient{
		DeleteFunc: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			deleteCalled = true
			return &service.DeleteResponse{
				Deleted:   true,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	mockClient := &mockSDKClient{
		kvClient: mockKV,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	deleteForce = false
	defer func() { deleteForce = false }()

	cmd := &cobra.Command{}
	err = runDeleteCommand(cmd, []string{"user", "john"})
	assert.NoError(t, err)
	assert.True(t, deleteCalled, "Delete should be called when user confirms")
}

// TestDeleteCommandConfirmationWithY tests delete confirmation with "y".
func TestDeleteCommandConfirmationWithY(t *testing.T) {
	// Save original stdin
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()

	// Create a pipe to simulate user input
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdin = r

	// Simulate user typing "y" (short form)
	go func() {
		w.Write([]byte("y\n"))
		w.Close()
	}()

	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	deleteCalled := false
	mockKV := &mockKVClient{
		DeleteFunc: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			deleteCalled = true
			return &service.DeleteResponse{
				Deleted:   true,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	mockClient := &mockSDKClient{
		kvClient: mockKV,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	deleteForce = false
	defer func() { deleteForce = false }()

	cmd := &cobra.Command{}
	err = runDeleteCommand(cmd, []string{"user", "john"})
	assert.NoError(t, err)
	assert.True(t, deleteCalled, "Delete should be called when user confirms with 'y'")
}

// TestRunListCommand tests the list command execution with various scenarios.
func TestRunListCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		flags         map[string]interface{}
		mockSetup     func(*mockKVClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful list with default limit",
			args: []string{"user"},
			flags: map[string]interface{}{
				"output": "table",
			},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					assert.Equal(t, "user", req.EntityType)
					assert.Equal(t, uint64(100), req.PageSize)
					assert.Equal(t, "", req.PageToken)
					return &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: []byte("value1"), Timestamp: time.Now().UnixNano()},
							{ID: 2, Data: []byte("value2"), Timestamp: time.Now().UnixNano()},
						},
						TotalCount: 2,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful list with custom limit",
			args: []string{"user"},
			flags: map[string]interface{}{
				"limit":  50,
				"output": "json",
			},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					assert.Equal(t, uint64(50), req.PageSize)
					return &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: []byte("value1"), Timestamp: time.Now().UnixNano()},
						},
						TotalCount: 1,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful list with cursor",
			args: []string{"user"},
			flags: map[string]interface{}{
				"cursor": "cursor-token",
				"output": "yaml",
			},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					assert.Equal(t, "cursor-token", req.PageToken)
					return &service.ListResponse{
						Entities:      []*service.Entity{},
						TotalCount:    0,
						NextPageToken: "next-cursor",
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful list with raw output",
			args: []string{"user"},
			flags: map[string]interface{}{
				"output": "raw",
			},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					return &service.ListResponse{
						Entities: []*service.Entity{
							{ID: 1, Data: []byte("value1"), Timestamp: time.Now().UnixNano()},
						},
						TotalCount: 1,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name:          "factory error",
			args:          []string{"user"},
			mockSetup:     func(m *mockKVClient) {},
			expectError:   true,
			errorContains: "connection refused",
		},
		{
			name: "client list error",
			args: []string{"user"},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "list", "storage error")
				}
			},
			expectError:   true,
			errorContains: "storage error",
		},
		{
			name: "invalid output format",
			args: []string{"user"},
			flags: map[string]interface{}{
				"output": "invalid",
			},
			mockSetup: func(m *mockKVClient) {
				m.ListFunc = func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
					return &service.ListResponse{
						Entities:   []*service.Entity{},
						TotalCount: 0,
					}, nil
				}
			},
			expectError:   true,
			errorContains: "unknown output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := sdkClientFactory
			defer func() { sdkClientFactory = origFactory }()

			// Reset the SDK client singleton for each test
			resetSDKClient()

			if tt.name == "factory error" {
				sdkClientFactory = func() (quicbaas.Client, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "factory", "connection refused")
				}
			} else {
				mockKV := &mockKVClient{}
				tt.mockSetup(mockKV)

				mockClient := &mockSDKClient{
					kvClient: mockKV,
				}

				sdkClientFactory = func() (quicbaas.Client, error) {
					return mockClient, nil
				}
			}

			// Set flags
			listLimit = 100 // default
			listCursor = ""
			if tt.flags != nil {
				for key, value := range tt.flags {
					switch key {
					case "limit":
						listLimit = value.(int)
					case "cursor":
						listCursor = value.(string)
					case "output":
						outputFormat = value.(string)
					}
				}
			}

			defer func() {
				listLimit = 100
				listCursor = ""
				outputFormat = "table"
			}()

			cmd := &cobra.Command{}
			err := runListCommand(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunQueryCommand tests the query command execution with various scenarios.
func TestRunQueryCommand(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		flags         map[string]interface{}
		mockSetup     func(*mockQueryClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful query with default limit",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index":  "email",
				"value":  "john@example.com",
				"output": "table",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					assert.Equal(t, "user", req.EntityType)
					assert.Equal(t, "email", req.Index)
					assert.Equal(t, "john@example.com", req.Value)
					assert.Equal(t, 100, req.Limit)
					return &query.QueryResponse{
						Results: []query.QueryResult{
							{
								Key:   "john",
								Value: []byte(`{"email":"john@example.com"}`),
							},
						},
						HasMore: false,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful query with custom limit",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index":  "email",
				"value":  "john@example.com",
				"limit":  50,
				"output": "json",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					assert.Equal(t, 50, req.Limit)
					return &query.QueryResponse{
						Results: []query.QueryResult{},
						HasMore: false,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful query with YAML output",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index":  "email",
				"value":  "john@example.com",
				"output": "yaml",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					return &query.QueryResponse{
						Results: []query.QueryResult{},
						HasMore: false,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "successful query with raw output",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index":  "email",
				"value":  "john@example.com",
				"output": "raw",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					return &query.QueryResponse{
						Results: []query.QueryResult{
							{Key: "john", Value: []byte("value")},
						},
						HasMore: false,
					}, nil
				}
			},
			expectError: false,
		},
		{
			name: "factory error",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index": "email",
				"value": "john@example.com",
			},
			mockSetup:     func(m *mockQueryClient) {},
			expectError:   true,
			errorContains: "connection refused",
		},
		{
			name: "client query error",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index": "email",
				"value": "john@example.com",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					return nil, errors.Newf(errors.ErrInternal, "query", "storage error")
				}
			},
			expectError:   true,
			errorContains: "storage error",
		},
		{
			name: "invalid output format",
			args: []string{"user"},
			flags: map[string]interface{}{
				"index":  "email",
				"value":  "john@example.com",
				"output": "invalid",
			},
			mockSetup: func(m *mockQueryClient) {
				m.ExecuteFunc = func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
					return &query.QueryResponse{
						Results: []query.QueryResult{},
						HasMore: false,
					}, nil
				}
			},
			expectError:   true,
			errorContains: "unknown output format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := sdkClientFactory
			defer func() { sdkClientFactory = origFactory }()

			// Reset the SDK client singleton for each test
			resetSDKClient()

			if tt.name == "factory error" {
				sdkClientFactory = func() (quicbaas.Client, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "factory", "connection refused")
				}
			} else {
				mockQuery := &mockQueryClient{}
				tt.mockSetup(mockQuery)

				mockClient := &mockSDKClient{
					queryClient: mockQuery,
				}

				sdkClientFactory = func() (quicbaas.Client, error) {
					return mockClient, nil
				}
			}

			// Set flags
			queryLimit = 100 // default
			if tt.flags != nil {
				for key, value := range tt.flags {
					switch key {
					case "index":
						queryIndex = value.(string)
					case "value":
						queryValue = value.(string)
					case "limit":
						queryLimit = value.(int)
					case "output":
						outputFormat = value.(string)
					}
				}
			}

			defer func() {
				queryIndex = ""
				queryValue = ""
				queryLimit = 100
				outputFormat = "table"
			}()

			cmd := &cobra.Command{}
			err := runQueryCommand(cmd, tt.args)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunPutCommandValidJSON tests put command with valid JSON and json flag.
func TestRunPutCommandValidJSON(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	validJSON := `{"name":"John Doe","email":"john@example.com","age":30}`

	mockKV := &mockKVClient{
		PutFunc: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			// Verify the JSON was accepted
			var data map[string]interface{}
			err := json.Unmarshal(req.Data, &data)
			assert.NoError(t, err)
			assert.Equal(t, "John Doe", data["name"])
			assert.Equal(t, "john@example.com", data["email"])
			assert.Equal(t, float64(30), data["age"])

			return &service.PutResponse{
				ID:        req.ID,
				Version:   1,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}

	mockClient := &mockSDKClient{
		kvClient: mockKV,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	jsonInput = true
	defer func() { jsonInput = false }()

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", validJSON})
	assert.NoError(t, err)
}

// TestListCommandWithNextCursor tests list command that returns a next cursor.
func TestListCommandWithNextCursor(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	mockKV := &mockKVClient{
		ListFunc: func(ctx context.Context, req *service.ListRequest) (*service.ListResponse, error) {
			return &service.ListResponse{
				Entities: []*service.Entity{
					{ID: 1, Data: []byte("value1"), Timestamp: time.Now().UnixNano()},
					{ID: 2, Data: []byte("value2"), Timestamp: time.Now().UnixNano()},
				},
				TotalCount:    2,
				NextPageToken: "next-page-token",
			}, nil
		},
	}

	mockClient := &mockSDKClient{
		kvClient: mockKV,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	cmd := &cobra.Command{}
	err := runListCommand(cmd, []string{"user"})
	assert.NoError(t, err)
}

// TestQueryCommandWithMultipleResults tests query returning multiple items.
func TestQueryCommandWithMultipleResults(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	mockQuery := &mockQueryClient{
		ExecuteFunc: func(ctx context.Context, req *query.QueryRequest) (*query.QueryResponse, error) {
			return &query.QueryResponse{
				Results: []query.QueryResult{
					{
						Key:   "john",
						Value: []byte(`{"email":"john@example.com"}`),
					},
					{
						Key:   "jane",
						Value: []byte(`{"email":"jane@example.com"}`),
					},
				},
				HasMore: false,
			}, nil
		},
	}

	mockClient := &mockSDKClient{
		queryClient: mockQuery,
	}

	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	queryIndex = "email"
	queryValue = "@example.com"
	defer func() {
		queryIndex = ""
		queryValue = ""
	}()

	cmd := &cobra.Command{}
	err := runQueryCommand(cmd, []string{"user"})
	assert.NoError(t, err)
}

// BenchmarkParseServiceConsistency benchmarks consistency parsing.
func BenchmarkParseServiceConsistency(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = parseServiceConsistency("local")
	}
}

// BenchmarkFormatError benchmarks error formatting.
func BenchmarkFormatError(b *testing.B) {
	err := assert.AnError

	for i := 0; i < b.N; i++ {
		_ = formatError(err)
	}
}

// TestQueryCommandMissingIndex tests query command with missing index flag.
func TestQueryCommandMissingIndex(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	mockClient := &mockSDKClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	queryIndex = ""
	queryValue = "john@example.com"
	defer func() {
		queryIndex = ""
		queryValue = ""
	}()

	cmd := &cobra.Command{}
	err := runQueryCommand(cmd, []string{"user"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index field is required")
}

// TestQueryCommandMissingValue tests query command with missing value flag.
func TestQueryCommandMissingValue(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	// Reset the SDK client singleton
	resetSDKClient()

	mockClient := &mockSDKClient{}
	sdkClientFactory = func() (quicbaas.Client, error) {
		return mockClient, nil
	}

	queryIndex = "email"
	queryValue = ""
	defer func() {
		queryIndex = ""
		queryValue = ""
	}()

	cmd := &cobra.Command{}
	err := runQueryCommand(cmd, []string{"user"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query value is required")
}

func TestDataCommandsExecution(t *testing.T) {
	tests := []struct {
		name        string
		cmd         *cobra.Command
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "put command missing args",
			cmd:         newPutCommand(),
			args:        []string{},
			expectError: true,
			errorMsg:    "arg(s)",
		},
		{
			name:        "get command missing args",
			cmd:         newGetCommand(),
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 2 arg(s)",
		},
		{
			name:        "delete command missing args",
			cmd:         newDeleteCommand(),
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 2 arg(s)",
		},
		{
			name:        "list command missing args",
			cmd:         newListCommand(),
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg(s)",
		},
		{
			name:        "query command missing args",
			cmd:         newQueryCommand(),
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.SetArgs(tt.args)
			var buf bytes.Buffer
			tt.cmd.SetOut(&buf)
			tt.cmd.SetErr(&buf)

			err := tt.cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTimeseriesCommandsExecution tests timeseries command execution paths
func TestVectorCommandsExecution(t *testing.T) {
	tests := []struct {
		name        string
		cmd         func() *cobra.Command
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "vector namespace create missing args",
			cmd:         newVectorNamespaceCreateCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg(s)",
		},
		{
			name:        "vector upsert missing args",
			cmd:         newVectorUpsertCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 2 arg(s)",
		},
		{
			name:        "vector batch upsert missing args",
			cmd:         newVectorBatchUpsertCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg(s)",
		},
		{
			name:        "vector search similar missing args",
			cmd:         newVectorSearchSimilarCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd()
			cmd.SetArgs(tt.args)
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestAuthCommandEdgeCases tests authentication command edge cases
func TestAuthCommandEdgeCases(t *testing.T) {
	t.Run("logout command with custom token file", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "token")

		// Create a token file
		token := &cli.TokenData{
			AccessToken:  "test-token",
			RefreshToken: "test-refresh",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			TokenType:    "Bearer",
		}
		err := cli.SaveToken(tokenFile, token)
		require.NoError(t, err)

		cmd := newAuthLogoutCommand()
		cmd.SetArgs([]string{"--token-file", tokenFile})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err = cmd.Execute()
		// Should succeed in deleting the token file
		assert.NoError(t, err)

		// Token file should be deleted
		_, err = os.Stat(tokenFile)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("status command with expired token", func(t *testing.T) {
		tmpDir := t.TempDir()
		tokenFile := filepath.Join(tmpDir, "token")

		// Create an expired token
		token := &cli.TokenData{
			AccessToken:  "expired-token",
			RefreshToken: "expired-refresh",
			ExpiresAt:    time.Now().Add(-1 * time.Hour), // Expired
			TokenType:    "Bearer",
		}
		err := cli.SaveToken(tokenFile, token)
		require.NoError(t, err)

		cmd := newAuthStatusCommand()
		cmd.SetArgs([]string{"--token-file", tokenFile, "--output", "json"})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err = cmd.Execute()
		// Should succeed and show expired status
		assert.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "expired")
	})
}

// TestTransactionCommandEdgeCases tests transaction command edge cases
func TestTransactionCommandEdgeCases(t *testing.T) {
	t.Run("txn compare invalid operator", func(t *testing.T) {
		cmd := newTxnCompareCommand()
		cmd.SetArgs([]string{
			"--condition", "key",
			"--operator", "INVALID",
			"--value", "test",
			"--then-put", "key2=value2",
			"--else-put", "key3=value3",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should error due to invalid client connection
		assert.Error(t, err)
	})

	t.Run("txn failure missing args", func(t *testing.T) {
		cmd := newTxnFailureCommand()
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts between 3 and 4 arg(s)")
	})

	t.Run("txn execute missing args", func(t *testing.T) {
		cmd := newTxnExecuteCommand()
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transaction has no operations")
	})

	t.Run("txn show command", func(t *testing.T) {
		cmd := newTxnShowCommand()
		cmd.SetArgs([]string{"tx123"})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect to server
		assert.Error(t, err)
	})
}

// TestCryptoCommandEdgeCases tests crypto command edge cases
func TestCryptoCommandEdgeCases(t *testing.T) {
	t.Run("crypto decrypt missing plaintext arg", func(t *testing.T) {
		cmd := newCryptoDecryptCommand()
		cmd.SetArgs([]string{"ciphertext-value", "--key-id", "test-key"})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect or validate
		assert.Error(t, err)
	})

	t.Run("crypto rewrap with all args", func(t *testing.T) {
		cmd := newCryptoRewrapCommand()
		cmd.SetArgs([]string{
			"ciphertext-value",
			"--key-id", "old-key",
			"--new-key-id", "new-key",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect
		assert.Error(t, err)
	})

	t.Run("crypto verify with signature", func(t *testing.T) {
		cmd := newCryptoVerifyCommand()
		cmd.SetArgs([]string{
			"test-data",
			"--signature", "test-signature",
			"--key-id", "test-key",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect
		assert.Error(t, err)
	})
}

// TestUpgradeCommandEdgeCases tests upgrade command edge cases
func TestUpgradeCommandEdgeCases(t *testing.T) {
	t.Run("upgrade start missing version arg", func(t *testing.T) {
		cmd := newUpgradeStartCommand()
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts 1 arg(s)")
	})
}

// TestCacheCommandValidation tests cache command flag validation
func TestCacheCommandValidation(t *testing.T) {
	t.Run("cache clear with invalid registry", func(t *testing.T) {
		cmd := newCacheClearCommand()
		cmd.SetArgs([]string{
			"--registry", "invalid-registry",
			"--pattern", "*",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect or validate
		assert.Error(t, err)
	})

	t.Run("cache enable missing registry", func(t *testing.T) {
		cmd := newCacheEnableCommand()
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts 1 arg(s)")
	})

	t.Run("cache disable missing registry", func(t *testing.T) {
		cmd := newCacheDisableCommand()
		cmd.SetArgs([]string{})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts 1 arg(s)")
	})
}

// TestBackupCommandValidation tests backup command flag validation
func TestBackupCommandValidation(t *testing.T) {
	t.Run("backup delete with force flag", func(t *testing.T) {
		cmd := newBackupDeleteCommand()
		cmd.SetArgs([]string{
			"backup-id-123",
			"--force",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect
		assert.Error(t, err)
	})
}

// TestKMSCommandValidation tests KMS command flag validation
func TestKMSCommandValidation(t *testing.T) {
	t.Run("kms import with all flags", func(t *testing.T) {
		cmd := newKMSKeyImportCommand()
		cmd.SetArgs([]string{
			"test-key-id",
			"--key-material", "base64-encoded-key",
			"--algorithm", "AES256",
		})

		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)

		err := cmd.Execute()
		// Should fail to connect
		assert.Error(t, err)
	})
}

// TestCommandHelperFunctions tests command helper utility functions
func TestCommandHelperFunctions(t *testing.T) {
	t.Run("createGRPCClient with invalid address", func(t *testing.T) {
		conn, err := createGRPCClient()
		// gRPC uses lazy connections - client creation succeeds even with invalid address
		assert.NoError(t, err)
		if conn != nil {
			_ = conn.Close()
		}
	})

	// Note: createClient, createVectorGRPCClient, and mock auth client tests not yet implemented
}

// TestFormatErrorFunction tests error formatting
func TestFormatErrorFunction(t *testing.T) {
	t.Run("format nil error", func(t *testing.T) {
		err := formatError(nil)
		assert.NoError(t, err)
	})

	t.Run("format standard error", func(t *testing.T) {
		origErr := assert.AnError
		err := formatError(origErr)
		assert.Error(t, err)
	})
}

// TestOutputFormatting tests various output format options
func TestOutputFormatting(t *testing.T) {
	formats := []string{"json", "yaml", "table", "raw"}

	t.Run("output formats validation", func(t *testing.T) {
		for _, format := range formats {
			cmd := newDataCommands()
			err := cmd.PersistentFlags().Set("output", format)
			assert.NoError(t, err)

			flag := cmd.PersistentFlags().Lookup("output")
			assert.Equal(t, format, flag.Value.String())
		}
	})
}

// TestCommandHelpOutput tests help text generation
func TestCommandHelpOutput(t *testing.T) {
	commands := []struct {
		name string
		cmd  func() *cobra.Command
	}{
		{"data", newDataCommands},
		{"auth", newAuthCommand},
		{"cache", newCacheCommands},
		{"backup", newBackupCommands},
		{"cluster", newClusterCommands},
		{"kms", newKMSCommand},
		{"barrier", newBarrierCommand},
		{"secrets", newSecretsCommand},
		{"transaction", newTransactionCommand},
	}

	for _, tc := range commands {
		t.Run(tc.name+" help output", func(t *testing.T) {
			cmd := tc.cmd()
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			cmd.SetArgs([]string{"--help"})

			err := cmd.Execute()
			assert.NoError(t, err)

			output := buf.String()
			assert.NotEmpty(t, output)
			assert.Contains(t, output, "Usage:")
		})
	}
}

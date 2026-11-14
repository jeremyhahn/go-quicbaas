//go:build timeseries

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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestNewTimeSeriesCommands(t *testing.T) {
	cmd := newTimeSeriesCommands()

	require.NotNil(t, cmd)
	assert.Equal(t, "timeseries", cmd.Use)
	assert.Contains(t, cmd.Aliases, "ts")
	assert.Contains(t, cmd.Short, "Time series")

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 7, "should have 7 subcommands")
	commandNames := make([]string, len(subcommands))
	for i, subcmd := range subcommands {
		commandNames[i] = subcmd.Name()
	}

	assert.Contains(t, commandNames, "write", "write command should be registered")
	assert.Contains(t, commandNames, "write-batch", "write-batch command should be registered")
	assert.Contains(t, commandNames, "query", "query command should be registered")
	assert.Contains(t, commandNames, "delete", "delete command should be registered")
	assert.Contains(t, commandNames, "get-retention", "get-retention command should be registered")
	assert.Contains(t, commandNames, "set-retention", "set-retention command should be registered")
	assert.Contains(t, commandNames, "list-metrics", "list-metrics command should be registered")
}

func TestTimeSeriesCommandFlags(t *testing.T) {
	cmd := newTimeSeriesCommands()

	// Test that persistent flags are available
	flags := cmd.PersistentFlags()

	// --timeout lives on the root command and is inherited (#246).
	assert.Nil(t, flags.Lookup("timeout"),
		"the timeseries group must not shadow the root --timeout")
	require.NotNil(t, rootCmd.PersistentFlags().Lookup("timeout"),
		"timeout flag should be on root command")

	outputFlag := flags.Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	tlsFlag := flags.Lookup("tls")
	require.NotNil(t, tlsFlag)

	maxRetriesFlag := flags.Lookup("max-retries")
	require.NotNil(t, maxRetriesFlag)
	assert.Equal(t, "3", maxRetriesFlag.DefValue)
}

func TestTSCommandFlags(t *testing.T) {
	tests := []struct {
		name  string
		cmd   *cobra.Command
		flags []string
	}{
		{name: "write", cmd: newTSWriteCommand(), flags: []string{"metric", "value", "tags", "timestamp"}},
		{name: "query", cmd: newTSQueryCommand(), flags: []string{"metric", "start", "end", "aggregation", "interval", "group-by", "limit"}},
		{name: "delete", cmd: newTSDeleteCommand(), flags: []string{"metric"}},
		{name: "set-retention", cmd: newTSSetRetentionCommand(), flags: []string{"duration", "resolution", "aggregation"}},
		{name: "list-metrics", cmd: newTSListMetricsCommand(), flags: []string{"prefix", "limit"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotNil(t, tt.cmd)
			for _, flag := range tt.flags {
				assert.NotNil(t, tt.cmd.Flags().Lookup(flag), "flag --%s should be registered", flag)
			}
		})
	}
}

// TestRunTSWriteCommand tests the write command with mocked client.
func TestRunTSWriteCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		metric       string
		value        float64
		tags         map[string]string
		timestamp    string
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful write with default timestamp",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						assert.Equal(t, 75.5, req.Value)
						assert.Equal(t, map[string]string{"host": "server1"}, req.Tags)
						return &cli.WritePointResponse{Success: true}, nil
					},
				}
			},
			metric:       "cpu.usage",
			value:        75.5,
			tags:         map[string]string{"host": "server1"},
			timestamp:    "",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "successful write with explicit timestamp",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
						assert.Equal(t, "memory.used", req.Metric)
						assert.Equal(t, 4096.0, req.Value)
						return &cli.WritePointResponse{Success: true}, nil
					},
				}
			},
			metric:       "memory.used",
			value:        4096.0,
			timestamp:    "2024-01-15T10:00:00Z",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "successful write with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
						assert.Equal(t, "disk.usage", req.Metric)
						assert.Equal(t, 85.0, req.Value)
						return &cli.WritePointResponse{Success: true}, nil
					},
				}
			},
			metric:       "disk.usage",
			value:        85.0,
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "invalid timestamp format",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			metric:      "cpu.usage",
			value:       75.5,
			timestamp:   "invalid-timestamp",
			expectError: true,
			errorMsg:    "invalid timestamp format",
		},
		{
			name: "write operation error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "write", "write failed")
					},
				}
			},
			metric:      "cpu.usage",
			value:       75.5,
			expectError: true,
			errorMsg:    "write failed",
		},
		{
			name: "write failure response",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						return &cli.WritePointResponse{Success: false}, nil
					},
				}
			},
			metric:       "cpu.usage",
			value:        75.5,
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSWriteCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set global flags AFTER command creation
			tsMetric = tt.metric
			tsValue = tt.value
			tsTags = tt.tags
			tsTimestamp = tt.timestamp
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSWriteCommand(cmd, []string{})

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			tsMetric = ""
			tsValue = 0
			tsTags = nil
			tsTimestamp = ""
			outputFormat = "table"
		})
	}
}

// TestRunTSWriteBatchCommand tests the batch write command.
func TestRunTSWriteBatchCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name        string
		setupMock   func() *MockTimeSeriesClient
		setupFile   func(t *testing.T) string
		expectError bool
		errorMsg    string
		checkOutput func(t *testing.T, stdout string)
	}{
		{
			name: "successful batch write",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
						assert.Len(t, req.Points, 2)
						assert.Equal(t, "cpu.usage", req.Points[0].Metric)
						assert.Equal(t, "cpu.usage", req.Points[1].Metric)
						return &cli.WriteBatchResponse{Success: true, PointsWritten: 2}, nil
					},
				}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "points.json")
				data := `[
					{"metric": "cpu.usage", "value": 75.5, "tags": {"host": "server1"}, "timestamp": "2024-01-15T10:00:00Z"},
					{"metric": "cpu.usage", "value": 82.3, "tags": {"host": "server2"}, "timestamp": "2024-01-15T10:00:00Z"}
				]`
				err := os.WriteFile(filePath, []byte(data), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "successful batch write with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
						return &cli.WriteBatchResponse{Success: true, PointsWritten: 2}, nil
					},
				}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "points.json")
				data := `[
					{"metric": "cpu.usage", "value": 75.5, "tags": {"host": "server1"}, "timestamp": "2024-01-15T10:00:00Z"},
					{"metric": "cpu.usage", "value": 82.3, "tags": {"host": "server2"}, "timestamp": "2024-01-15T10:00:00Z"}
				]`
				err := os.WriteFile(filePath, []byte(data), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "file read error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			setupFile: func(t *testing.T) string {
				return "/nonexistent/file.json"
			},
			expectError: true,
			errorMsg:    "failed to read file",
		},
		{
			name: "invalid JSON",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "invalid.json")
				err := os.WriteFile(filePath, []byte("invalid json"), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: true,
			errorMsg:    "invalid JSON",
		},
		{
			name: "batch write error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "batch-write", "batch write failed")
					},
				}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "points.json")
				data := `[{"metric": "cpu.usage", "value": 75.5, "timestamp": "2024-01-15T10:00:00Z"}]`
				err := os.WriteFile(filePath, []byte(data), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: true,
			errorMsg:    "batch write failed",
		},
		{
			name: "batch write with invalid timestamps uses default",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
						// Verify timestamps are set
						for _, point := range req.Points {
							assert.False(t, point.Timestamp.IsZero())
						}
						return &cli.WriteBatchResponse{Success: true, PointsWritten: 1}, nil
					},
				}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "points.json")
				data := `[{"metric": "cpu.usage", "value": 75.5, "timestamp": "invalid"}]`
				err := os.WriteFile(filePath, []byte(data), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: false,
		},
		{
			name: "batch write failure response",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
						return &cli.WriteBatchResponse{Success: false, PointsWritten: 0}, nil
					},
				}
			},
			setupFile: func(t *testing.T) string {
				tmpDir := t.TempDir()
				filePath := filepath.Join(tmpDir, "points.json")
				data := `[{"metric": "cpu.usage", "value": 75.5, "timestamp": "2024-01-15T10:00:00Z"}]`
				err := os.WriteFile(filePath, []byte(data), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Setup file
			filePath := tt.setupFile(t)

			// Set output format
			if tt.name == "successful batch write with JSON output" {
				outputFormat = "json"
			} else {
				outputFormat = "table"
			}

			// Create command
			cmd := newTSWriteBatchCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute
			err := runTSWriteBatchCommand(cmd, []string{filePath})

			// Assert
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			outputFormat = "table"
		})
	}
}

// TestRunTSQueryCommand tests the query command.
func TestRunTSQueryCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		metric       string
		startTime    string
		endTime      string
		tags         map[string]string
		aggregation  string
		interval     int32
		groupBy      []string
		limit        int
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful query",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSQueryFunc: func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						assert.NotNil(t, req.StartTime)
						assert.NotNil(t, req.EndTime)
						return &cli.TSQueryResponse{
							Metric: "cpu.usage",
							Points: []cli.TSDataPoint{
								{
									Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
									Value:     75.5,
									Tags:      map[string]string{"host": "server1"},
								},
							},
						}, nil
					},
				}
			},
			metric:       "cpu.usage",
			startTime:    "2024-01-15T09:00:00Z",
			endTime:      "2024-01-15T10:00:00Z",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "query with aggregation",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSQueryFunc: func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
						assert.Equal(t, "avg", req.Aggregation)
						assert.Equal(t, int32(300), req.IntervalSec)
						aggValue := 75.5
						return &cli.TSQueryResponse{
							Metric:          "cpu.usage",
							AggregatedValue: &aggValue,
							Points:          []cli.TSDataPoint{},
						}, nil
					},
				}
			},
			metric:       "cpu.usage",
			startTime:    "2024-01-15T09:00:00Z",
			endTime:      "2024-01-15T10:00:00Z",
			aggregation:  "avg",
			interval:     300,
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "query with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSQueryFunc: func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
						return &cli.TSQueryResponse{
							Metric: "cpu.usage",
							Points: []cli.TSDataPoint{},
						}, nil
					},
				}
			},
			metric:       "cpu.usage",
			startTime:    "2024-01-15T09:00:00Z",
			endTime:      "2024-01-15T10:00:00Z",
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "invalid start time",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			metric:      "cpu.usage",
			startTime:   "invalid",
			endTime:     "2024-01-15T10:00:00Z",
			expectError: true,
			errorMsg:    "invalid start time format",
		},
		{
			name: "invalid end time",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			metric:      "cpu.usage",
			startTime:   "2024-01-15T09:00:00Z",
			endTime:     "invalid",
			expectError: true,
			errorMsg:    "invalid end time format",
		},
		{
			name: "query error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSQueryFunc: func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "query", "query failed")
					},
				}
			},
			metric:      "cpu.usage",
			startTime:   "2024-01-15T09:00:00Z",
			endTime:     "2024-01-15T10:00:00Z",
			expectError: true,
			errorMsg:    "query failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSQueryCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set global flags AFTER command creation
			tsMetric = tt.metric
			tsStartTime = tt.startTime
			tsEndTime = tt.endTime
			tsTags = tt.tags
			tsAggregation = tt.aggregation
			tsInterval = tt.interval
			tsGroupBy = tt.groupBy
			tsLimit = tt.limit
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSQueryCommand(cmd, []string{})

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			tsMetric = ""
			tsStartTime = ""
			tsEndTime = ""
			tsTags = nil
			tsAggregation = ""
			tsInterval = 0
			tsGroupBy = nil
			tsLimit = 0
			outputFormat = "table"
		})
	}
}

// TestRunTSDeleteCommand tests the delete command.
func TestRunTSDeleteCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		metric       string
		tags         map[string]string
		startTime    string
		endTime      string
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful delete without time range",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						assert.Nil(t, req.StartTime)
						assert.Nil(t, req.EndTime)
						return &cli.TSDeleteResponse{Success: true}, nil
					},
				}
			},
			metric:       "cpu.usage",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "successful delete with time range",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						assert.NotNil(t, req.StartTime)
						assert.NotNil(t, req.EndTime)
						return &cli.TSDeleteResponse{Success: true}, nil
					},
				}
			},
			metric:       "cpu.usage",
			startTime:    "2024-01-01T00:00:00Z",
			endTime:      "2024-01-02T00:00:00Z",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "successful delete with tags",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						assert.Equal(t, map[string]string{"host": "server1"}, req.Tags)
						return &cli.TSDeleteResponse{Success: true}, nil
					},
				}
			},
			metric:       "cpu.usage",
			tags:         map[string]string{"host": "server1"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "delete with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						return &cli.TSDeleteResponse{Success: true}, nil
					},
				}
			},
			metric:       "cpu.usage",
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "invalid start time",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			metric:      "cpu.usage",
			startTime:   "invalid",
			endTime:     "2024-01-02T00:00:00Z",
			expectError: true,
			errorMsg:    "invalid start time format",
		},
		{
			name: "invalid end time",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{}
			},
			metric:      "cpu.usage",
			startTime:   "2024-01-01T00:00:00Z",
			endTime:     "invalid",
			expectError: true,
			errorMsg:    "invalid end time format",
		},
		{
			name: "delete error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "delete", "delete failed")
					},
				}
			},
			metric:      "cpu.usage",
			expectError: true,
			errorMsg:    "delete failed",
		},
		{
			name: "delete failure response",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
						return &cli.TSDeleteResponse{Success: false}, nil
					},
				}
			},
			metric:       "cpu.usage",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSDeleteCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set global flags AFTER command creation
			tsMetric = tt.metric
			tsTags = tt.tags
			tsStartTime = tt.startTime
			tsEndTime = tt.endTime
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSDeleteCommand(cmd, []string{})

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			tsMetric = ""
			tsTags = nil
			tsStartTime = ""
			tsEndTime = ""
			outputFormat = "table"
		})
	}
}

// TestRunTSGetRetentionCommand tests the get retention policy command.
func TestRunTSGetRetentionCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		args         []string
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful get retention",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSGetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						return &cli.TSGetRetentionPolicyResponse{
							Metric: "cpu.usage",
							Policy: &cli.TSRetentionPolicy{
								Duration:    30 * 24 * time.Hour,
								Resolution:  5 * time.Minute,
								Aggregation: "avg",
							},
						}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "get retention with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSGetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
						return &cli.TSGetRetentionPolicyResponse{
							Metric: "cpu.usage",
							Policy: &cli.TSRetentionPolicy{
								Duration:    30 * 24 * time.Hour,
								Resolution:  5 * time.Minute,
								Aggregation: "avg",
							},
						}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "no retention policy set",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSGetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
						return &cli.TSGetRetentionPolicyResponse{
							Metric: "cpu.usage",
							Policy: nil,
						}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "get retention error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSGetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "get-retention", "get failed")
					},
				}
			},
			args:        []string{"cpu.usage"},
			expectError: true,
			errorMsg:    "get failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSGetRetentionCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set output format AFTER command creation
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSGetRetentionCommand(cmd, tt.args)

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			outputFormat = "table"
		})
	}
}

// TestRunTSSetRetentionCommand tests the set retention policy command.
func TestRunTSSetRetentionCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		args         []string
		duration     int64
		resolution   int64
		aggregation  string
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful set retention",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSSetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
						assert.Equal(t, "cpu.usage", req.Metric)
						assert.NotNil(t, req.Policy)
						assert.Equal(t, 2592000*time.Second, req.Policy.Duration)
						assert.Equal(t, 300*time.Second, req.Policy.Resolution)
						assert.Equal(t, "avg", req.Policy.Aggregation)
						return &cli.TSSetRetentionPolicyResponse{Success: true}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			duration:     2592000,
			resolution:   300,
			aggregation:  "avg",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "set retention with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSSetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
						return &cli.TSSetRetentionPolicyResponse{Success: true}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			duration:     2592000,
			resolution:   300,
			aggregation:  "avg",
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "set retention error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSSetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "set-retention", "set failed")
					},
				}
			},
			args:        []string{"cpu.usage"},
			duration:    2592000,
			expectError: true,
			errorMsg:    "set failed",
		},
		{
			name: "set retention failure response",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSSetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
						return &cli.TSSetRetentionPolicyResponse{Success: false}, nil
					},
				}
			},
			args:         []string{"cpu.usage"},
			duration:     2592000,
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSSetRetentionCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set global flags AFTER command creation
			tsDurationSec = tt.duration
			tsResolutionSec = tt.resolution
			tsAggregation = tt.aggregation
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSSetRetentionCommand(cmd, tt.args)

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			tsDurationSec = 0
			tsResolutionSec = 0
			tsAggregation = ""
			outputFormat = "table"
		})
	}
}

// TestRunTSListMetricsCommand tests the list metrics command.
func TestRunTSListMetricsCommand(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	tests := []struct {
		name         string
		setupMock    func() *MockTimeSeriesClient
		prefix       string
		limit        int
		cursor       string
		outputFormat string
		expectError  bool
		errorMsg     string
		checkOutput  func(t *testing.T, stdout string)
	}{
		{
			name: "successful list all metrics",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
						assert.Equal(t, "", req.Prefix)
						assert.Equal(t, 100, req.Limit)
						return &cli.TSListMetricsResponse{
							Metrics: []string{"cpu.usage", "memory.used", "disk.usage"},
						}, nil
					},
				}
			},
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "list metrics with prefix",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
						assert.Equal(t, "cpu", req.Prefix)
						return &cli.TSListMetricsResponse{
							Metrics: []string{"cpu.usage", "cpu.temperature"},
						}, nil
					},
				}
			},
			prefix:       "cpu",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "list metrics with pagination",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
						assert.Equal(t, 50, req.Limit)
						assert.Equal(t, "next_page", req.Cursor)
						return &cli.TSListMetricsResponse{
							Metrics:    []string{"metric1", "metric2"},
							NextCursor: "next_page_2",
						}, nil
					},
				}
			},
			limit:        50,
			cursor:       "next_page",
			outputFormat: "table",
			expectError:  false,
			// Note: Output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "list metrics with JSON output",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
						return &cli.TSListMetricsResponse{
							Metrics: []string{"cpu.usage", "memory.used"},
						}, nil
					},
				}
			},
			outputFormat: "json",
			expectError:  false,
			// Note: JSON output goes to os.Stdout, not captured by cmd.SetOut
		},
		{
			name: "list metrics error",
			setupMock: func() *MockTimeSeriesClient {
				return &MockTimeSeriesClient{
					TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "list-metrics", "list failed")
					},
				}
			},
			expectError: true,
			errorMsg:    "list failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock
			mockClient := tt.setupMock()
			timeSeriesClientFactory = func() (TimeSeriesClient, error) {
				return mockClient, nil
			}

			// Create command first (this binds flags to variables with defaults)
			cmd := newTSListMetricsCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Set global flags AFTER command creation
			tsPrefix = tt.prefix
			if tt.limit > 0 {
				tsLimit = tt.limit
			} else {
				tsLimit = 100
			}
			tsCursor = tt.cursor
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute
			err := runTSListMetricsCommand(cmd, []string{})

			// Assert
			if tt.expectError {
				if assert.Error(t, err) && tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				if tt.checkOutput != nil {
					tt.checkOutput(t, stdout.String())
				}
			}

			// Reset flags
			tsPrefix = ""
			tsLimit = 0
			tsCursor = ""
			outputFormat = "table"
		})
	}
}

// TestFormatTSWriteResponse tests write response formatting.
func TestFormatTSWriteResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.WritePointResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSWriteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format success", func(t *testing.T) {
		resp := &cli.WritePointResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSWriteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format failure", func(t *testing.T) {
		resp := &cli.WritePointResponse{
			Success:   false,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSWriteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSWriteResponse(&cli.WritePointResponse{Success: true, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTSWriteBatchResponse tests batch write response formatting.
func TestFormatTSWriteBatchResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.WriteBatchResponse{
			Success:       true,
			PointsWritten: 10,
			Timestamp:     time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSWriteBatchResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format success", func(t *testing.T) {
		resp := &cli.WriteBatchResponse{
			Success:       true,
			PointsWritten: 10,
			Timestamp:     time.Now(),
		}
		outputFormat = "table"
		err := formatTSWriteBatchResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format failure", func(t *testing.T) {
		resp := &cli.WriteBatchResponse{
			Success:       false,
			PointsWritten: 0,
			Timestamp:     time.Now(),
		}
		outputFormat = "table"
		err := formatTSWriteBatchResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSWriteBatchResponse(&cli.WriteBatchResponse{Success: true, PointsWritten: 5, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTSQueryResponse tests query response formatting.
func TestFormatTSQueryResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.TSQueryResponse{
			Metric: "cpu_usage",
			Tags:   map[string]string{"host": "server1"},
			Points: []cli.TSDataPoint{
				{Timestamp: time.Now(), Value: 50.5},
			},
			Timestamp: time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSQueryResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format with points", func(t *testing.T) {
		resp := &cli.TSQueryResponse{
			Metric: "cpu_usage",
			Tags:   map[string]string{"host": "server1"},
			Points: []cli.TSDataPoint{
				{Timestamp: time.Now(), Value: 50.5},
				{Timestamp: time.Now(), Value: 60.5},
			},
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSQueryResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format empty", func(t *testing.T) {
		resp := &cli.TSQueryResponse{
			Metric:    "cpu_usage",
			Points:    []cli.TSDataPoint{},
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSQueryResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format with aggregated value", func(t *testing.T) {
		aggValue := 75.5
		resp := &cli.TSQueryResponse{
			Metric:          "cpu_usage",
			Tags:            map[string]string{"host": "server1", "region": "us-east"},
			Points:          []cli.TSDataPoint{},
			AggregatedValue: &aggValue,
			Timestamp:       time.Now(),
		}
		outputFormat = "table"
		err := formatTSQueryResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format with points and tags", func(t *testing.T) {
		resp := &cli.TSQueryResponse{
			Metric: "cpu_usage",
			Tags:   map[string]string{"host": "server1"},
			Points: []cli.TSDataPoint{
				{Timestamp: time.Now(), Value: 50.5, Tags: map[string]string{"cpu": "0", "core": "1"}},
				{Timestamp: time.Now(), Value: 60.5, Tags: map[string]string{"cpu": "1"}},
			},
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSQueryResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSQueryResponse(&cli.TSQueryResponse{Metric: "cpu.usage", Points: []cli.TSDataPoint{}, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTags tests the formatTags helper function.
func TestFormatTags(t *testing.T) {
	t.Run("empty tags", func(t *testing.T) {
		result := formatTags(nil)
		assert.Equal(t, "", result)
	})

	t.Run("single tag", func(t *testing.T) {
		result := formatTags(map[string]string{"host": "server1"})
		assert.Contains(t, result, "host=server1")
	})

	t.Run("multiple tags", func(t *testing.T) {
		result := formatTags(map[string]string{"host": "server1", "region": "us-east"})
		assert.Contains(t, result, "host=server1")
		assert.Contains(t, result, "region=us-east")
	})
}

// TestFormatTSDeleteResponse tests delete response formatting.
func TestFormatTSDeleteResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.TSDeleteResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSDeleteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format success", func(t *testing.T) {
		resp := &cli.TSDeleteResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSDeleteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format failure", func(t *testing.T) {
		resp := &cli.TSDeleteResponse{
			Success:   false,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSDeleteResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSDeleteResponse(&cli.TSDeleteResponse{Success: true, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTSGetRetentionResponse tests get retention response formatting.
func TestFormatTSGetRetentionResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.TSGetRetentionPolicyResponse{
			Metric: "cpu_usage",
			Policy: &cli.TSRetentionPolicy{
				Duration:    24 * time.Hour,
				Resolution:  time.Minute,
				Aggregation: "avg",
			},
			Timestamp: time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSGetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format with policy", func(t *testing.T) {
		resp := &cli.TSGetRetentionPolicyResponse{
			Metric: "cpu_usage",
			Policy: &cli.TSRetentionPolicy{
				Duration:    24 * time.Hour,
				Resolution:  time.Minute,
				Aggregation: "avg",
			},
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSGetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format no policy", func(t *testing.T) {
		resp := &cli.TSGetRetentionPolicyResponse{
			Metric:    "cpu_usage",
			Policy:    nil,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSGetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSGetRetentionResponse(&cli.TSGetRetentionPolicyResponse{Metric: "cpu.usage", Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTSSetRetentionResponse tests set retention response formatting.
func TestFormatTSSetRetentionResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.TSSetRetentionPolicyResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSSetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format success", func(t *testing.T) {
		resp := &cli.TSSetRetentionPolicyResponse{
			Success:   true,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSSetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format failure", func(t *testing.T) {
		resp := &cli.TSSetRetentionPolicyResponse{
			Success:   false,
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSSetRetentionResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSSetRetentionResponse(&cli.TSSetRetentionPolicyResponse{Success: true, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestFormatTSListMetricsResponse tests list metrics response formatting.
func TestFormatTSListMetricsResponse(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		resp := &cli.TSListMetricsResponse{
			Metrics:    []string{"cpu_usage", "memory_usage", "disk_usage"},
			NextCursor: "",
			Timestamp:  time.Now(),
		}
		outputFormat = "json"
		defer func() { outputFormat = "table" }()
		err := formatTSListMetricsResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format with metrics", func(t *testing.T) {
		resp := &cli.TSListMetricsResponse{
			Metrics:    []string{"cpu_usage", "memory_usage", "disk_usage"},
			NextCursor: "cursor123",
			Timestamp:  time.Now(),
		}
		outputFormat = "table"
		err := formatTSListMetricsResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("table format empty", func(t *testing.T) {
		resp := &cli.TSListMetricsResponse{
			Metrics:   []string{},
			Timestamp: time.Now(),
		}
		outputFormat = "table"
		err := formatTSListMetricsResponse(resp)
		assert.NoError(t, err)
	})

	t.Run("invalid format", func(t *testing.T) {
		outputFormat = "invalid_format_xyz"
		defer func() { outputFormat = "table" }()
		err := formatTSListMetricsResponse(&cli.TSListMetricsResponse{Metrics: []string{"cpu.usage", "mem.used"}, Timestamp: time.Now()})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid_format_xyz")
	})
}

// TestTimeSeriesCommandsFactoryError tests factory error handling.
func TestTimeSeriesCommandsFactoryError(t *testing.T) {
	// Save original factory
	origFactory := timeSeriesClientFactory
	defer func() { timeSeriesClientFactory = origFactory }()

	// Setup factory to return error
	timeSeriesClientFactory = func() (TimeSeriesClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "factory", "factory error")
	}

	tests := []struct {
		name    string
		command func() *cobra.Command
		runFunc func(cmd *cobra.Command, args []string) error
		args    []string
	}{
		{
			name:    "write command factory error",
			command: newTSWriteCommand,
			runFunc: runTSWriteCommand,
			args:    []string{},
		},
		{
			name:    "query command factory error",
			command: newTSQueryCommand,
			runFunc: runTSQueryCommand,
			args:    []string{},
		},
		{
			name:    "delete command factory error",
			command: newTSDeleteCommand,
			runFunc: runTSDeleteCommand,
			args:    []string{},
		},
		{
			name:    "get-retention command factory error",
			command: newTSGetRetentionCommand,
			runFunc: runTSGetRetentionCommand,
			args:    []string{"cpu.usage"},
		},
		{
			name:    "set-retention command factory error",
			command: newTSSetRetentionCommand,
			runFunc: runTSSetRetentionCommand,
			args:    []string{"cpu.usage"},
		},
		{
			name:    "list-metrics command factory error",
			command: newTSListMetricsCommand,
			runFunc: runTSListMetricsCommand,
			args:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set minimal required flags
			tsMetric = "test.metric"
			tsStartTime = "2024-01-15T09:00:00Z"
			tsEndTime = "2024-01-15T10:00:00Z"
			tsDurationSec = 3600

			cmd := tt.command()
			err := tt.runFunc(cmd, tt.args)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "factory error")

			// Reset flags
			tsMetric = ""
			tsStartTime = ""
			tsEndTime = ""
			tsDurationSec = 0
		})
	}
}

// TestCreateGRPCClientLazyConnection tests that createGRPCClient uses lazy connections.
// The createGRPCClient function does not validate TLS settings at creation time.
// gRPC clients use lazy connections - connection only happens on first RPC call.
func TestCreateGRPCClientLazyConnection(t *testing.T) {
	// Save original values
	origTLSEnabled := tlsEnabled
	origTLSCert := tlsCert
	origTLSKey := tlsKey
	origAddress := address
	defer func() {
		tlsEnabled = origTLSEnabled
		tlsCert = origTLSCert
		tlsKey = origTLSKey
		address = origAddress
	}()

	// Test that client creation succeeds even with TLS enabled but no certs
	// because gRPC uses lazy connections and createGRPCClient doesn't validate TLS settings
	tlsEnabled = true
	tlsCert = ""
	tlsKey = ""
	address = "localhost:50051"

	client, err := createGRPCClient()
	// createGRPCClient uses lazy connections - client creation succeeds
	// The connection would only fail when making an actual RPC call
	assert.NoError(t, err)
	if client != nil {
		_ = client.Close()
	}
}

func TestMockCryptoClient_FuncFieldPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("KMSGenerateHMAC_WithFunc", func(t *testing.T) {
		expected := &service.HMACResponse{KeyID: "key-1", HMAC: []byte("custom-hmac")}
		client := &MockCryptoClient{
			KMSGenerateHMACFunc: func(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
				return expected, nil
			},
		}
		resp, err := client.KMSGenerateHMAC(ctx, &service.HMACRequest{KeyID: "key-1", Message: []byte("data")})
		assert.NoError(t, err)
		assert.Equal(t, expected, resp)
	})

	t.Run("KMSVerifyHMAC_WithFunc", func(t *testing.T) {
		expected := &service.VerifyHMACResponse{KeyID: "key-2", Valid: false}
		client := &MockCryptoClient{
			KMSVerifyHMACFunc: func(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
				return expected, nil
			},
		}
		resp, err := client.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{KeyID: "key-2"})
		assert.NoError(t, err)
		assert.Equal(t, expected, resp)
	})

	t.Run("Close_WithFunc", func(t *testing.T) {
		closed := false
		client := &MockCryptoClient{
			CloseFunc: func() error {
				closed = true
				return nil
			},
		}
		err := client.Close()
		assert.NoError(t, err)
		assert.True(t, closed)
	})
}

func TestMockKMSClient_FuncFieldPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("KMSClearKeySafety_WithFunc", func(t *testing.T) {
		expected := &service.ClearKeySafetyResponse{}
		client := &MockKMSClient{
			KMSClearKeySafetyFunc: func(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
				return expected, nil
			},
		}
		resp, err := client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{KeyID: "key-1"})
		assert.NoError(t, err)
		assert.Equal(t, expected, resp)
	})

	t.Run("Close_WithFunc", func(t *testing.T) {
		closed := false
		client := &MockKMSClient{
			CloseFunc: func() error {
				closed = true
				return nil
			},
		}
		err := client.Close()
		assert.NoError(t, err)
		assert.True(t, closed)
	})
}

func TestMockSecretsClient_FuncFieldPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("UpdateSecretStatus_WithFunc", func(t *testing.T) {
		expected := &service.UpdateSecretStatusResponse{SecretID: "secret-1"}
		client := &MockSecretsClient{
			UpdateSecretStatusFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return expected, nil
			},
		}
		resp, err := client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "secret-1"})
		assert.NoError(t, err)
		assert.Equal(t, expected, resp)
	})

	t.Run("Close_WithFunc", func(t *testing.T) {
		closed := false
		client := &MockSecretsClient{
			CloseFunc: func() error {
				closed = true
				return nil
			},
		}
		err := client.Close()
		assert.NoError(t, err)
		assert.True(t, closed)
	})
}

// =============================================================================
// Object format function branch coverage
// =============================================================================

func TestFormatObjectGetResponse_JSONNoMetadata(t *testing.T) {
	orig := objectOutputFormat
	origMeta := objectIncludeMeta
	defer func() {
		objectOutputFormat = orig
		objectIncludeMeta = origMeta
	}()
	objectOutputFormat = "json"
	objectIncludeMeta = false

	resp := &pb.GetObjectResponse{
		Key:         "bucket/no-meta",
		Found:       true,
		Value:       []byte("data"),
		Size:        4,
		Version:     1,
		ContentType: "text/plain",
		Checksum:    []byte{0xAB, 0xCD},
		Tags:        map[string]string{"env": "test"},
		Metadata:    &pb.ObjectMetadata{Owner: "admin"},
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_YAMLFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.GetObjectPolicyResponse{
		Key: "bucket/yaml-policy",
		Policy: &pb.ObjectPolicy{
			RetentionPolicy: &pb.RetentionPolicy{
				Enabled:       true,
				RetentionDays: 30,
			},
		},
	}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatOllamaEmbedBatchResponse_YAMLFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = "yaml"

	resp := &pb.OllamaEmbedBatchResponse{
		Model:      "nomic-embed-text",
		Count:      2,
		Dimensions: 384,
		Embeddings: []*pb.OllamaEmbeddingVector{
			{Values: []float32{0.1, 0.2}},
			{Values: []float32{0.3, 0.4}},
		},
	}
	err := formatOllamaEmbedBatchResponse(resp)
	assert.NoError(t, err)
}

func TestFormatOllamaEmbedBatchResponse_DefaultEmptyEmbedding(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = ""

	resp := &pb.OllamaEmbedBatchResponse{
		Model:      "nomic-embed-text",
		Count:      2,
		Dimensions: 384,
		Embeddings: []*pb.OllamaEmbeddingVector{
			{Values: []float32{0.1, 0.2, 0.3, 0.4}},
			nil,
		},
	}
	err := formatOllamaEmbedBatchResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// Embedding format function JSON branch coverage
// =============================================================================

func TestFormatEmbeddingSetDefaultResponse_JSONFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = "json"

	resp := &embedding.SetDefaultProviderResponse{
		Provider:         "openai",
		PreviousProvider: "ollama",
		Timestamp:        time.Now(),
	}
	err := formatEmbeddingSetDefaultResponse(resp)
	assert.NoError(t, err)
}

func TestFormatEmbeddingCacheClearResponse_JSONFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = "json"

	resp := &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: 42,
		Provider:       "openai",
		Timestamp:      time.Now(),
	}
	err := formatEmbeddingCacheClearResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// Object lifecycle default format with replication policy coverage
// =============================================================================

func TestFormatObjectLifecycleResponse_DefaultWithReplicationPolicy(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.GetObjectPolicyResponse{
		Policy: &pb.ObjectPolicy{
			ReplicationPolicy: &pb.ReplicationPolicy{
				Enabled:      true,
				MinReplicas:  3,
				Mode:         pb.ReplicationMode_REPLICATION_MODE_ASYNC,
				TargetStores: []string{"s3-backup", "gcs-archive"},
			},
			ArchivePolicy: &pb.ArchivePolicy{
				Enabled:           true,
				ArchiveAfterDays:  30,
				TargetStore:       "glacier",
				DeleteFromPrimary: false,
			},
		},
	}
	err := formatObjectLifecycleResponse("test-bucket", resp)
	assert.NoError(t, err)
}

func TestParseGracePeriodErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "negative days", input: "-5d", wantErr: "grace period cannot be negative"},
		{name: "invalid duration without day suffix", input: "abc", wantErr: "invalid duration format"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := parseGracePeriod(tt.input)
			assert.Error(t, err)
			assert.Zero(t, duration)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTimeseriesCommandsExecution(t *testing.T) {
	tests := []struct {
		name        string
		cmd         func() *cobra.Command
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "ts write missing args",
			cmd:         newTSWriteCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "ts query missing args",
			cmd:         newTSQueryCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "ts delete missing args",
			cmd:         newTSDeleteCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "ts set-retention missing args",
			cmd:         newTSSetRetentionCommand,
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts",
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

// TestVectorCommandsExecution tests vector command execution paths

// TestCreateClientHelpers tests client creation helper functions

// TestConvertSearchByIDResponses tests search response conversion

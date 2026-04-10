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
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
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

	timeoutFlag := flags.Lookup("timeout")
	require.NotNil(t, timeoutFlag)

	outputFlag := flags.Lookup("output")
	require.NotNil(t, outputFlag)
	assert.Equal(t, "table", outputFlag.DefValue)

	tlsFlag := flags.Lookup("tls")
	require.NotNil(t, tlsFlag)

	maxRetriesFlag := flags.Lookup("max-retries")
	require.NotNil(t, maxRetriesFlag)
	assert.Equal(t, "3", maxRetriesFlag.DefValue)
}

func TestTSWriteCommand(t *testing.T) {
	cmd := newTSWriteCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "write", cmd.Use)
	assert.Contains(t, cmd.Short, "Write a single")

	// Verify flags exist
	metricFlag := cmd.Flags().Lookup("metric")
	require.NotNil(t, metricFlag)

	valueFlag := cmd.Flags().Lookup("value")
	require.NotNil(t, valueFlag)

	tagsFlag := cmd.Flags().Lookup("tags")
	require.NotNil(t, tagsFlag)

	timestampFlag := cmd.Flags().Lookup("timestamp")
	require.NotNil(t, timestampFlag)
}

func TestTSWriteCommandHelp(t *testing.T) {
	cmd := newTSWriteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "write")
	assert.Contains(t, output, "--metric")
	assert.Contains(t, output, "--value")
	assert.Contains(t, output, "--tags")
	assert.Contains(t, output, "--timestamp")
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
			name: "client creation error",
			setupMock: func() *MockTimeSeriesClient {
				return nil
			},
			metric:      "cpu.usage",
			value:       75.5,
			expectError: true,
			errorMsg:    "mock error",
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
			if mockClient != nil {
				timeSeriesClientFactory = func() (TimeSeriesClient, error) {
					return mockClient, nil
				}
			} else {
				timeSeriesClientFactory = func() (TimeSeriesClient, error) {
					return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
				}
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

func TestTSWriteBatchCommand(t *testing.T) {
	cmd := newTSWriteBatchCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "write-batch <json-file>", cmd.Use)
	assert.Contains(t, cmd.Short, "Write multiple")
}

func TestTSWriteBatchCommandHelp(t *testing.T) {
	cmd := newTSWriteBatchCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "write-batch")
	assert.Contains(t, output, "json-file")
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

func TestTSQueryCommand(t *testing.T) {
	cmd := newTSQueryCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "query", cmd.Use)
	assert.Contains(t, cmd.Short, "Query time series")

	// Verify flags exist
	metricFlag := cmd.Flags().Lookup("metric")
	require.NotNil(t, metricFlag)

	startTimeFlag := cmd.Flags().Lookup("start")
	require.NotNil(t, startTimeFlag)

	endTimeFlag := cmd.Flags().Lookup("end")
	require.NotNil(t, endTimeFlag)

	aggFlag := cmd.Flags().Lookup("aggregation")
	require.NotNil(t, aggFlag)

	intervalFlag := cmd.Flags().Lookup("interval")
	require.NotNil(t, intervalFlag)

	groupByFlag := cmd.Flags().Lookup("group-by")
	require.NotNil(t, groupByFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	require.NotNil(t, limitFlag)
}

func TestTSQueryCommandHelp(t *testing.T) {
	cmd := newTSQueryCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "query")
	assert.Contains(t, output, "--metric")
	assert.Contains(t, output, "--start")
	assert.Contains(t, output, "--end")
	assert.Contains(t, output, "--aggregation")
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

func TestTSDeleteCommand(t *testing.T) {
	cmd := newTSDeleteCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "delete", cmd.Use)
	assert.Contains(t, cmd.Short, "Delete")

	// Verify flags exist
	metricFlag := cmd.Flags().Lookup("metric")
	require.NotNil(t, metricFlag)
}

func TestTSDeleteCommandHelp(t *testing.T) {
	cmd := newTSDeleteCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "delete")
	assert.Contains(t, output, "--metric")
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

func TestTSGetRetentionCommand(t *testing.T) {
	cmd := newTSGetRetentionCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "get-retention <metric>", cmd.Use)
	assert.Contains(t, cmd.Short, "Get retention")
}

func TestTSGetRetentionCommandHelp(t *testing.T) {
	cmd := newTSGetRetentionCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "get-retention")
	assert.Contains(t, output, "<metric>")
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

func TestTSSetRetentionCommand(t *testing.T) {
	cmd := newTSSetRetentionCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "set-retention <metric>", cmd.Use)
	assert.Contains(t, cmd.Short, "Set retention")

	// Verify flags exist
	durationFlag := cmd.Flags().Lookup("duration")
	require.NotNil(t, durationFlag)

	resolutionFlag := cmd.Flags().Lookup("resolution")
	require.NotNil(t, resolutionFlag)

	aggFlag := cmd.Flags().Lookup("aggregation")
	require.NotNil(t, aggFlag)
}

func TestTSSetRetentionCommandHelp(t *testing.T) {
	cmd := newTSSetRetentionCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "set-retention")
	assert.Contains(t, output, "<metric>")
	assert.Contains(t, output, "--duration")
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

func TestTSListMetricsCommand(t *testing.T) {
	cmd := newTSListMetricsCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "list-metrics", cmd.Use)
	assert.Contains(t, cmd.Short, "List")

	// Verify flags exist
	prefixFlag := cmd.Flags().Lookup("prefix")
	require.NotNil(t, prefixFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	require.NotNil(t, limitFlag)
}

func TestTSListMetricsCommandHelp(t *testing.T) {
	cmd := newTSListMetricsCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "list-metrics")
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

func TestTimeSeriesCommandsIntegration(t *testing.T) {
	// Test that timeseries commands hierarchy is correct
	tsCmd := newTimeSeriesCommands()

	// Verify command hierarchy
	subcommands := tsCmd.Commands()
	assert.Len(t, subcommands, 7, "should have 7 subcommands")

	// Verify each subcommand has a valid parent set
	for _, subcmd := range subcommands {
		assert.NotNil(t, subcmd.Parent(), "subcommand should have parent set")
	}
}

func TestTimeSeriesCommandAliases(t *testing.T) {
	cmd := newTimeSeriesCommands()

	// Test that alias works
	assert.Contains(t, cmd.Aliases, "ts")
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

func TestMockSealClient_Close_WithFunc(t *testing.T) {
	closed := false
	client := &MockSealClient{
		CloseFunc: func() error {
			closed = true
			return nil
		},
	}
	err := client.Close()
	assert.NoError(t, err)
	assert.True(t, closed)
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

func TestMockTimeSeriesClient_FuncFieldPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("TSWritePoint_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSWritePointFunc: func(ctx context.Context, req *cli.WritePointRequest) (*cli.WritePointResponse, error) {
				return &cli.WritePointResponse{Success: true}, nil
			},
		}
		resp, err := client.TSWritePoint(ctx, &cli.WritePointRequest{})
		assert.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("TSWriteBatch_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSWriteBatchFunc: func(ctx context.Context, req *cli.WriteBatchRequest) (*cli.WriteBatchResponse, error) {
				return &cli.WriteBatchResponse{PointsWritten: 10}, nil
			},
		}
		resp, err := client.TSWriteBatch(ctx, &cli.WriteBatchRequest{})
		assert.NoError(t, err)
		assert.Equal(t, 10, resp.PointsWritten)
	})

	t.Run("TSQuery_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSQueryFunc: func(ctx context.Context, req *cli.TSQueryRequest) (*cli.TSQueryResponse, error) {
				return &cli.TSQueryResponse{Metric: "cpu"}, nil
			},
		}
		resp, err := client.TSQuery(ctx, &cli.TSQueryRequest{})
		assert.NoError(t, err)
		assert.Equal(t, "cpu", resp.Metric)
	})

	t.Run("TSDeleteSeries_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSDeleteSeriesFunc: func(ctx context.Context, req *cli.TSDeleteRequest) (*cli.TSDeleteResponse, error) {
				return &cli.TSDeleteResponse{Success: true}, nil
			},
		}
		resp, err := client.TSDeleteSeries(ctx, &cli.TSDeleteRequest{})
		assert.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("TSGetRetentionPolicy_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSGetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSGetRetentionPolicyRequest) (*cli.TSGetRetentionPolicyResponse, error) {
				return &cli.TSGetRetentionPolicyResponse{Metric: "cpu"}, nil
			},
		}
		resp, err := client.TSGetRetentionPolicy(ctx, &cli.TSGetRetentionPolicyRequest{})
		assert.NoError(t, err)
		assert.Equal(t, "cpu", resp.Metric)
	})

	t.Run("TSSetRetentionPolicy_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSSetRetentionPolicyFunc: func(ctx context.Context, req *cli.TSSetRetentionPolicyRequest) (*cli.TSSetRetentionPolicyResponse, error) {
				return &cli.TSSetRetentionPolicyResponse{Success: true}, nil
			},
		}
		resp, err := client.TSSetRetentionPolicy(ctx, &cli.TSSetRetentionPolicyRequest{})
		assert.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("TSListMetrics_WithFunc", func(t *testing.T) {
		client := &MockTimeSeriesClient{
			TSListMetricsFunc: func(ctx context.Context, req *cli.TSListMetricsRequest) (*cli.TSListMetricsResponse, error) {
				return &cli.TSListMetricsResponse{Metrics: []string{"cpu", "mem"}}, nil
			},
		}
		resp, err := client.TSListMetrics(ctx, &cli.TSListMetricsRequest{})
		assert.NoError(t, err)
		assert.Len(t, resp.Metrics, 2)
	})

	t.Run("Close_WithFunc", func(t *testing.T) {
		closed := false
		client := &MockTimeSeriesClient{
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

func TestMockRBACClient_ListRoles_WithFunc(t *testing.T) {
	ctx := context.Background()
	expected := &rbac.ListRolesResponse{
		Roles: []*rbac.RoleInfo{
			{RoleID: "custom-role", RoleName: "editor"},
		},
		TotalCount: 1,
	}
	client := &MockRBACClient{
		ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
			return expected, nil
		},
	}
	resp, err := client.ListRoles(ctx, &rbac.ListRolesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, expected, resp)
}

// =============================================================================
// Scheduler format function YAML/default branch coverage
// =============================================================================

func TestFormatSchedulerJobListResponse_YAMLFormat(t *testing.T) {
	resp := &service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{
				ID:      1,
				Name:    "yaml-job",
				Type:    service.SchedulerJobTypeRecurring,
				Handler: "backup.full",
				Status:  service.SchedulerJobStatusPending,
			},
		},
		TotalCount: 1,
	}
	var buf bytes.Buffer
	err := formatSchedulerJobListResponse(resp, "yaml", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "yaml-job")
}

func TestFormatSchedulerJobListResponse_DefaultFormat(t *testing.T) {
	resp := &service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{
				ID:     10,
				Name:   "default-job",
				Type:   service.SchedulerJobTypeOneShot,
				Status: service.SchedulerJobStatusCompleted,
			},
		},
		TotalCount: 1,
	}
	var buf bytes.Buffer
	err := formatSchedulerJobListResponse(resp, "", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "default-job")
}

func TestFormatSchedulerJobTriggerResponse_YAMLFormat(t *testing.T) {
	resp := &service.TriggerSchedulerJobResponse{
		JobID:  42,
		Status: "triggered",
	}
	var buf bytes.Buffer
	err := formatSchedulerJobTriggerResponse(resp, "yaml", &buf)
	assert.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "42")
}

func TestFormatSchedulerJobTriggerResponse_DefaultFormat(t *testing.T) {
	resp := &service.TriggerSchedulerJobResponse{
		JobID:  99,
		Status: "triggered",
	}
	var buf bytes.Buffer
	err := formatSchedulerJobTriggerResponse(resp, "", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "99")
	assert.Contains(t, buf.String(), "triggered")
}

func TestFormatSchedulerJobHistoryResponse_DefaultFormat(t *testing.T) {
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{
				ID:        1,
				Status:    service.SchedulerJobStatusCompleted,
				StartedAt: time.Now(),
				Duration:  5 * time.Second,
			},
		},
	}
	var buf bytes.Buffer
	err := formatSchedulerJobHistoryResponse(resp, "", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "completed")
}

func TestFormatSchedulerJobHistoryResponse_YAMLFormat(t *testing.T) {
	resp := &service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{
				ID:        2,
				Status:    service.SchedulerJobStatusFailed,
				StartedAt: time.Now(),
				Duration:  1 * time.Second,
				Error:     "timeout",
			},
		},
	}
	var buf bytes.Buffer
	err := formatSchedulerJobHistoryResponse(resp, "yaml", &buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "timeout")
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

// =============================================================================
// parseGracePeriod additional edge case coverage
// =============================================================================
// These tests cover the negative days error path (line 210) and the
// time.ParseDuration error path (line 222) that are not covered by existing tests.

func TestParseGracePeriod_NegativeDays(t *testing.T) {
	// Input "-5d" ends with "d", so it enters the days parsing branch.
	// Sscanf parses -5 successfully, then the negative check at line 210 triggers.
	duration, err := parseGracePeriod("-5d")
	assert.Error(t, err)
	assert.Zero(t, duration)
	assert.Contains(t, err.Error(), "grace period cannot be negative")
}

func TestParseGracePeriod_InvalidDurationNoDaySuffix(t *testing.T) {
	// Input "abc" does NOT end with "d", so it bypasses the days branch and
	// falls through to time.ParseDuration("abc") which returns an error.
	// This covers the error path at line 222.
	duration, err := parseGracePeriod("abc")
	assert.Error(t, err)
	assert.Zero(t, duration)
	assert.Contains(t, err.Error(), "invalid duration format")
}

// =============================================================================
// Messaging format function default branch coverage
// =============================================================================
// The formatChannelInfoResponse function has a default switch case at line 1244
// that prints a simple channel name. All other branches (json, yaml, table) are
// covered, but the default/"raw" path was not exercised.

func TestFormatChannelInfoResponse_DefaultFormat(t *testing.T) {
	channelResp := &grpc.MessagingGetChannelResponse{
		Channel: &grpc.MessagingChannel{
			Name:       "test-channel",
			BufferSize: 100,
			Persistent: true,
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatChannelInfoResponse(
		channelResp,
		nil,   // statsResp
		nil,   // statsErr
		nil,   // dedupResp
		nil,   // dedupErr
		"raw", // format - triggers default branch
		&buf,
	)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "test-channel")
}

func TestFormatTSWriteResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}

	err := formatTSWriteResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSWriteBatchResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.WriteBatchResponse{
		Success:       true,
		PointsWritten: 5,
		Timestamp:     time.Now(),
	}

	err := formatTSWriteBatchResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSDeleteResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.TSDeleteResponse{
		Success:   true,
		Timestamp: time.Now(),
	}

	err := formatTSDeleteResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSSetRetentionResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.TSSetRetentionPolicyResponse{
		Success:   true,
		Timestamp: time.Now(),
	}

	err := formatTSSetRetentionResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSListMetricsResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.TSListMetricsResponse{
		Metrics:   []string{"cpu.usage", "mem.used"},
		Timestamp: time.Now(),
	}

	err := formatTSListMetricsResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSGetRetentionResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.TSGetRetentionPolicyResponse{
		Metric:    "cpu.usage",
		Timestamp: time.Now(),
	}

	err := formatTSGetRetentionResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

func TestFormatTSQueryResponse_InvalidFormat(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "invalid_format_xyz"

	resp := &cli.TSQueryResponse{
		Metric:    "cpu.usage",
		Points:    []cli.TSDataPoint{},
		Timestamp: time.Now(),
	}

	err := formatTSQueryResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_format_xyz")
}

// --------------------------------------------------------------------------
// Format function success paths covering specific branches
// --------------------------------------------------------------------------

func TestFormatTSWriteResponse_TableSuccess(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.WritePointResponse{
		Success:   true,
		Timestamp: time.Now(),
	}

	err := formatTSWriteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatTSWriteResponse_TableFailure(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.WritePointResponse{
		Success:   false,
		Timestamp: time.Now(),
	}

	err := formatTSWriteResponse(resp)
	assert.NoError(t, err)
}

func TestFormatTSWriteBatchResponse_TableFailure(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.WriteBatchResponse{
		Success:       false,
		PointsWritten: 0,
		Timestamp:     time.Now(),
	}

	err := formatTSWriteBatchResponse(resp)
	assert.NoError(t, err)
}

func TestFormatTSListMetricsResponse_TableWithCursor(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.TSListMetricsResponse{
		Metrics:    []string{"cpu.usage", "mem.used"},
		NextCursor: "cursor-abc",
		Timestamp:  time.Now(),
	}

	err := formatTSListMetricsResponse(resp)
	assert.NoError(t, err)
}

func TestFormatTSGetRetentionResponse_TableWithPolicy(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.TSGetRetentionPolicyResponse{
		Metric: "cpu.usage",
		Policy: &cli.TSRetentionPolicy{
			Duration:    30 * 24 * time.Hour,
			Resolution:  time.Minute,
			Aggregation: "avg",
		},
		Timestamp: time.Now(),
	}

	err := formatTSGetRetentionResponse(resp)
	assert.NoError(t, err)
}

func TestFormatTSGetRetentionResponse_TableNoPolicy(t *testing.T) {
	savedFormat := outputFormat
	defer func() { outputFormat = savedFormat }()

	outputFormat = "table"

	resp := &cli.TSGetRetentionPolicyResponse{
		Metric:    "cpu.usage",
		Policy:    nil,
		Timestamp: time.Now(),
	}

	err := formatTSGetRetentionResponse(resp)
	assert.NoError(t, err)
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

func TestRunCommandsWithInvalidServer(t *testing.T) {
	// Save original address
	originalAddress := address
	defer func() { address = originalAddress }()

	// Set invalid address to trigger connection errors
	address = "invalid-server:12345"

	tests := []struct {
		name        string
		setupCmd    func() error
		description string
	}{
		{
			name: "runPutCommand",
			setupCmd: func() error {
				cmd := newPutCommand()
				cmd.SetArgs([]string{"entity", "test-key", "test-value"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test put command with connection failure",
		},
		{
			name: "runGetCommand",
			setupCmd: func() error {
				cmd := newGetCommand()
				cmd.SetArgs([]string{"entity", "test-key"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test get command with connection failure",
		},
		{
			name: "runDeleteCommand",
			setupCmd: func() error {
				cmd := newDeleteCommand()
				cmd.SetArgs([]string{"entity", "test-key"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test delete command with connection failure",
		},
		{
			name: "runListCommand",
			setupCmd: func() error {
				cmd := newListCommand()
				cmd.SetArgs([]string{"entity"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test list command with connection failure",
		},
		{
			name: "runQueryCommand",
			setupCmd: func() error {
				cmd := newQueryCommand()
				cmd.SetArgs([]string{"entity", "--index", "test-index", "--value", "test-value"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test query command with connection failure",
		},
		{
			name: "runTSWriteCommand",
			setupCmd: func() error {
				cmd := newTSWriteCommand()
				cmd.SetArgs([]string{"test-metric", "100.5"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries write command with connection failure",
		},
		{
			name: "runTSWriteBatchCommand",
			setupCmd: func() error {
				cmd := newTSWriteBatchCommand()
				cmd.SetArgs([]string{`[{"metric":"test","value":100}]`})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries batch write command with connection failure",
		},
		{
			name: "runTSQueryCommand",
			setupCmd: func() error {
				cmd := newTSQueryCommand()
				cmd.SetArgs([]string{"test-metric"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries query command with connection failure",
		},
		{
			name: "runTSDeleteCommand",
			setupCmd: func() error {
				cmd := newTSDeleteCommand()
				cmd.SetArgs([]string{"test-metric"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries delete command with connection failure",
		},
		{
			name: "runTSGetRetentionCommand",
			setupCmd: func() error {
				cmd := newTSGetRetentionCommand()
				cmd.SetArgs([]string{"test-metric"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries get retention command with connection failure",
		},
		{
			name: "runTSSetRetentionCommand",
			setupCmd: func() error {
				cmd := newTSSetRetentionCommand()
				cmd.SetArgs([]string{"test-metric", "7d"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries set retention command with connection failure",
		},
		{
			name: "runTSListMetricsCommand",
			setupCmd: func() error {
				cmd := newTSListMetricsCommand()
				cmd.SetArgs([]string{})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test timeseries list metrics command with connection failure",
		},
		{
			name: "runVectorNamespaceCreateCommand",
			setupCmd: func() error {
				cmd := newVectorNamespaceCreateCommand()
				cmd.SetArgs([]string{"test-ns", "--dimension", "128"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector namespace create command with connection failure",
		},
		{
			name: "runVectorNamespaceDeleteCommand",
			setupCmd: func() error {
				cmd := newVectorNamespaceDeleteCommand()
				cmd.SetArgs([]string{"test-ns"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector namespace delete command with connection failure",
		},
		{
			name: "runVectorNamespaceListCommand",
			setupCmd: func() error {
				cmd := newVectorNamespaceListCommand()
				cmd.SetArgs([]string{})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector namespace list command with connection failure",
		},
		{
			name: "runVectorNamespaceInfoCommand",
			setupCmd: func() error {
				cmd := newVectorNamespaceInfoCommand()
				cmd.SetArgs([]string{"test-ns"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector namespace info command with connection failure",
		},
		{
			name: "runVectorUpsertCommand",
			setupCmd: func() error {
				cmd := newVectorUpsertCommand()
				cmd.SetArgs([]string{"test-ns", "vec-id", "0.1,0.2,0.3"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector upsert command with connection failure",
		},
		{
			name: "runVectorBatchUpsertCommand",
			setupCmd: func() error {
				cmd := newVectorBatchUpsertCommand()
				cmd.SetArgs([]string{`[{"id":"v1","vector":[0.1,0.2]}]`})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector batch upsert command with connection failure",
		},
		{
			name: "runVectorGetCommand",
			setupCmd: func() error {
				cmd := newVectorGetCommand()
				cmd.SetArgs([]string{"test-ns", "vec-id"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector get command with connection failure",
		},
		{
			name: "runVectorDeleteCommand",
			setupCmd: func() error {
				cmd := newVectorDeleteCommand()
				cmd.SetArgs([]string{"test-ns", "vec-id"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector delete command with connection failure",
		},
		{
			name: "runVectorBatchDeleteCommand",
			setupCmd: func() error {
				cmd := newVectorBatchDeleteCommand()
				cmd.SetArgs([]string{"test-ns", "vec-1,vec-2,vec-3"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector batch delete command with connection failure",
		},
		{
			name: "runVectorListCommand",
			setupCmd: func() error {
				cmd := newVectorListCommand()
				cmd.SetArgs([]string{"test-ns"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector list command with connection failure",
		},
		{
			name: "runVectorSearchSimilarCommand",
			setupCmd: func() error {
				cmd := newVectorSearchSimilarCommand()
				cmd.SetArgs([]string{"test-ns", "0.1,0.2,0.3"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector search similar command with connection failure",
		},
		{
			name: "runVectorSearchByIDCommand",
			setupCmd: func() error {
				cmd := newVectorSearchByIDCommand()
				cmd.SetArgs([]string{"test-ns", "vec-id"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector search by ID command with connection failure",
		},
		{
			name: "runVectorIndexRebuildCommand",
			setupCmd: func() error {
				cmd := newVectorIndexRebuildCommand()
				cmd.SetArgs([]string{"test-ns"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector index rebuild command with connection failure",
		},
		{
			name: "runVectorIndexStatsCommand",
			setupCmd: func() error {
				cmd := newVectorIndexStatsCommand()
				cmd.SetArgs([]string{"test-ns"})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test vector index stats command with connection failure",
		},
		{
			name: "runSealSealCommand",
			setupCmd: func() error {
				cmd := newSealSealCommand()
				cmd.SetArgs([]string{})
				var buf bytes.Buffer
				cmd.SetOut(&buf)
				cmd.SetErr(&buf)
				return cmd.Execute()
			},
			description: "Test seal command with connection failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setupCmd()
			// All these commands should error because they can't connect to a server
			// or due to missing required arguments/validation
			assert.Error(t, err, tt.description)
			// Just verify we get some kind of error - the specific error type varies
			// depending on whether the command has required flags, needs confirmation, etc.
		})
	}
}

// TestCreateClientHelpers tests client creation helper functions
func TestFormatFunctions(t *testing.T) {
	t.Run("formatTags with empty tags", func(t *testing.T) {
		result := formatTags(nil)
		assert.Equal(t, "", result)
	})

	t.Run("formatTags with tags", func(t *testing.T) {
		tags := map[string]string{
			"env":     "prod",
			"service": "api",
		}
		result := formatTags(tags)
		// Should contain both tags
		assert.Contains(t, result, "env=prod")
		assert.Contains(t, result, "service=api")
	})
}

// TestConvertSearchByIDResponses tests search response conversion

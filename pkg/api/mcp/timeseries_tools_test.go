//go:build mcp

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestTimeSeriesTools_Registration verifies all TimeSeries tools are registered
func TestTimeSeriesTools_Registration(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	registry := NewToolRegistry(mockService, log)
	tools := registry.ListTools()

	expectedTimeSeriesTools := []string{
		"qrdb_ts_write_point",
		"qrdb_ts_write_batch",
		"qrdb_ts_query",
		"qrdb_ts_delete_series",
		"qrdb_ts_get_retention_policy",
		"qrdb_ts_set_retention_policy",
		"qrdb_ts_list_metrics",
	}

	for _, expectedTool := range expectedTimeSeriesTools {
		found := false
		for _, tool := range tools {
			if tool.Name == expectedTool {
				found = true
				assert.NotEmpty(t, tool.Description, "Tool %s should have a description", expectedTool)
				assert.Equal(t, "object", tool.InputSchema["type"], "Tool %s schema should be object type", expectedTool)
				break
			}
		}
		assert.True(t, found, "Tool %s should be registered", expectedTool)
	}
}

// TestTimeSeriesTools_SchemaValidation verifies tool input schemas are valid
func TestTimeSeriesTools_SchemaValidation(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	registry := NewToolRegistry(mockService, log)
	tools := registry.ListTools()

	// Build tool map
	toolMap := make(map[string]ToolDefinition)
	for _, tool := range tools {
		toolMap[tool.Name] = tool
	}

	tests := []struct {
		toolName       string
		requiredFields []string
		optionalFields []string
	}{
		{
			toolName:       "qrdb_ts_write_point",
			requiredFields: []string{"metric", "value"},
			optionalFields: []string{"timestamp", "tags"},
		},
		{
			toolName:       "qrdb_ts_write_batch",
			requiredFields: []string{"points"},
			optionalFields: []string{},
		},
		{
			toolName:       "qrdb_ts_query",
			requiredFields: []string{"metric", "start_time", "end_time"},
			optionalFields: []string{"tags", "aggregation", "interval_seconds", "group_by", "limit", "percentile"},
		},
		{
			toolName:       "qrdb_ts_delete_series",
			requiredFields: []string{"metric"},
			optionalFields: []string{"tags", "start_time", "end_time"},
		},
		{
			toolName:       "qrdb_ts_get_retention_policy",
			requiredFields: []string{"metric"},
			optionalFields: []string{},
		},
		{
			toolName:       "qrdb_ts_set_retention_policy",
			requiredFields: []string{"metric", "duration_seconds"},
			optionalFields: []string{"resolution_seconds", "aggregation"},
		},
		{
			toolName:       "qrdb_ts_list_metrics",
			requiredFields: []string{},
			optionalFields: []string{"prefix", "tags", "limit", "cursor"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			tool, exists := toolMap[tt.toolName]
			require.True(t, exists, "Tool %s should exist", tt.toolName)

			// Get properties from schema
			properties, ok := tool.InputSchema["properties"].(map[string]interface{})
			require.True(t, ok, "Tool %s should have properties", tt.toolName)

			// Verify required fields are in schema
			for _, reqField := range tt.requiredFields {
				_, exists := properties[reqField]
				assert.True(t, exists, "Required field %s should be in schema for %s", reqField, tt.toolName)
			}

			// Verify optional fields are in schema
			for _, optField := range tt.optionalFields {
				_, exists := properties[optField]
				assert.True(t, exists, "Optional field %s should be in schema for %s", optField, tt.toolName)
			}
		})
	}
}

// TestTimeSeriesTools_NoDuplicateNames verifies tool names are unique
func TestTimeSeriesTools_NoDuplicateNames(t *testing.T) {
	log, err := logger.New(logger.Default())
	require.NoError(t, err)

	mockService := testutil.NewMockQuicBaaS()
	registry := NewToolRegistry(mockService, log)
	tools := registry.ListTools()

	seen := make(map[string]bool)
	for _, tool := range tools {
		assert.False(t, seen[tool.Name], "Tool name %s should be unique", tool.Name)
		seen[tool.Name] = true
	}
}

// =============================================================================
// TimeSeries Execution Tests
// =============================================================================

func newTSRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(svc, log)
}

func TestTimeSeriesWritePoint_ExecuteSuccess(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "basic write",
			args: map[string]interface{}{"metric": "cpu.usage", "value": "85.5"},
		},
		{
			name: "with timestamp",
			args: map[string]interface{}{
				"metric": "mem.used", "value": "1024.0",
				"timestamp": float64(1700000000000000000),
			},
		},
		{
			name: "with tags",
			args: map[string]interface{}{
				"metric": "disk.io", "value": "42.1",
				"tags": `{"host":"node-1","region":"us-east"}`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "qrdb_ts_write_point", tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestTimeSeriesWritePoint_ExecuteErrors(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing metric", map[string]interface{}{"value": "1.0"}, "metric"},
		{"missing value", map[string]interface{}{"metric": "cpu"}, "value"},
		{"empty value", map[string]interface{}{"metric": "cpu", "value": ""}, "value"},
		{"invalid value", map[string]interface{}{"metric": "cpu", "value": "abc"}, "invalid numeric"},
		{"bad tags JSON", map[string]interface{}{"metric": "cpu", "value": "1.0", "tags": "{"}, "invalid JSON"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "qrdb_ts_write_point", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestTimeSeriesWriteBatch_ExecuteSuccess(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "qrdb_ts_write_batch", map[string]interface{}{
		"points": `[{"metric":"cpu","value":"85.5"},{"metric":"mem","value":"1024","timestamp":1700000000000000000,"tags":{"host":"n1"}}]`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestTimeSeriesWriteBatch_ExecuteErrors(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{"missing points", map[string]interface{}{}, "points"},
		{"invalid JSON", map[string]interface{}{"points": "bad"}, "invalid JSON"},
		{"non-object tags in point", map[string]interface{}{"points": `[{"metric":"cpu","value":"85.5","tags":"not-a-map"}]`}, "invalid JSON array"},
		{"empty array", map[string]interface{}{"points": "[]"}, "at least one"},
		{"missing metric in point", map[string]interface{}{"points": `[{"value":"1.0"}]`}, "metric is required"},
		{"invalid value in point", map[string]interface{}{"points": `[{"metric":"cpu","value":"bad"}]`}, "invalid numeric"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, "qrdb_ts_write_batch", tc.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestTimeSeriesQuery_Success(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "required params only",
			args: map[string]interface{}{
				"metric":     "cpu.usage",
				"start_time": float64(1700000000000000000),
				"end_time":   float64(1700000001000000000),
			},
		},
		{
			name: "all optional params",
			args: map[string]interface{}{
				"metric":           "cpu.usage",
				"start_time":       float64(1700000000000000000),
				"end_time":         float64(1700000001000000000),
				"tags":             `{"host":"server-1","region":"us-east"}`,
				"aggregation":      "avg",
				"interval_seconds": float64(60),
				"group_by":         `["host","region"]`,
				"limit":            float64(100),
				"percentile":       "95.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_query", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestTimeSeriesQuery_InvalidArgs(t *testing.T) {
	validRange := func(extra map[string]interface{}) map[string]interface{} {
		args := map[string]interface{}{
			"metric":     "cpu.usage",
			"start_time": float64(1700000000000000000),
			"end_time":   float64(1700000001000000000),
		}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}

	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains []string
	}{
		{
			name:        "missing metric",
			args:        map[string]interface{}{},
			errContains: []string{"metric"},
		},
		{
			name: "missing start_time",
			args: map[string]interface{}{
				"metric":   "cpu.usage",
				"end_time": float64(1700000001000000000),
			},
			errContains: []string{"start_time"},
		},
		{
			name: "missing end_time",
			args: map[string]interface{}{
				"metric":     "cpu.usage",
				"start_time": float64(1700000000000000000),
			},
			errContains: []string{"end_time"},
		},
		{
			name:        "invalid tags JSON",
			args:        validRange(map[string]interface{}{"tags": "{not-valid-json"}),
			errContains: []string{"invalid JSON"},
		},
		{
			name:        "invalid group_by JSON",
			args:        validRange(map[string]interface{}{"group_by": "not-valid-json"}),
			errContains: []string{"group_by", "invalid JSON array"},
		},
		{
			name:        "invalid percentile",
			args:        validRange(map[string]interface{}{"percentile": "not-a-number"}),
			errContains: []string{"percentile", "invalid numeric value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_query", tt.args)
			require.Error(t, err)
			assert.Nil(t, result)
			for _, want := range tt.errContains {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestTimeSeriesDeleteSeries_Success(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "metric only",
			args: map[string]interface{}{"metric": "old.metric"},
		},
		{
			name: "time range",
			args: map[string]interface{}{
				"metric":     "old.metric",
				"start_time": float64(1700000000000000000),
				"end_time":   float64(1700000001000000000),
			},
		},
		{
			name: "start_time only",
			args: map[string]interface{}{
				"metric":     "old.metric",
				"start_time": float64(1700000000000000000),
			},
		},
		{
			name: "end_time only",
			args: map[string]interface{}{
				"metric":   "old.metric",
				"end_time": float64(1700000001000000000),
			},
		},
		{
			name: "tags",
			args: map[string]interface{}{
				"metric": "old.metric",
				"tags":   `{"host":"decommissioned-server"}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_delete_series", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestTimeSeriesDeleteSeries_InvalidArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]interface{}
		errContains string
	}{
		{
			name:        "missing metric",
			args:        map[string]interface{}{},
			errContains: "metric",
		},
		{
			name: "invalid tags JSON",
			args: map[string]interface{}{
				"metric": "old.metric",
				"tags":   "{invalid-json",
			},
			errContains: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_delete_series", tt.args)
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestTimeSeriesRetention_GetSuccess(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "qrdb_ts_get_retention_policy", map[string]interface{}{
		"metric": "cpu.usage",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestTimeSeriesRetention_GetMissingMetric(t *testing.T) {
	registry := newTSRegistry(t)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "qrdb_ts_get_retention_policy", map[string]interface{}{})
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestTimeSeriesRetention_SetSuccess(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "duration",
			args: map[string]interface{}{
				"metric":           "cpu.usage",
				"duration_seconds": float64(86400),
			},
		},
		{
			name: "all options",
			args: map[string]interface{}{
				"metric":             "cpu.usage",
				"duration_seconds":   float64(86400 * 30),
				"resolution_seconds": float64(60),
				"aggregation":        "avg",
			},
		},
		{
			// The schema marks duration_seconds as required, but the
			// implementation defaults it to 0 and accepts that.
			name: "duration omitted defaults to zero",
			args: map[string]interface{}{"metric": "cpu.usage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_set_retention_policy", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestTimeSeriesRetention_SetMissingMetric(t *testing.T) {
	registry := newTSRegistry(t)

	result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_set_retention_policy", map[string]interface{}{
		"duration_seconds": float64(86400),
	})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "metric")
}

func TestTimeSeriesListMetrics_Success(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "no params",
			args: map[string]interface{}{},
		},
		{
			name: "all params",
			args: map[string]interface{}{
				"prefix": "cpu.",
				"tags":   `{"host":"server-1"}`,
				"limit":  float64(50),
				"cursor": "cursor-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newTSRegistry(t)
			result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_list_metrics", tt.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// listMetricsSpy counts the ListMetrics calls that reach the service, on top
// of the package mock.
type listMetricsSpy struct {
	*mockService
	calls int
}

func (s *listMetricsSpy) ListMetrics(ctx context.Context, req *service.TSListMetricsRequest) (*service.TSListMetricsResponse, error) {
	s.calls++
	return s.mockService.ListMetrics(ctx, req)
}

// TestTimeSeriesListMetrics_WithInvalidTags pins the rejection of malformed
// tags: the tool answers an error and no result, and the request never
// reaches the service. The well-formed case proves the spy counts a call that
// does reach it.
func TestTimeSeriesListMetrics_WithInvalidTags(t *testing.T) {
	newSpyRegistry := func(t *testing.T) (*ToolRegistry, *listMetricsSpy) {
		t.Helper()
		spy := &listMetricsSpy{mockService: &mockService{}}
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)
		return NewToolRegistry(spy, log), spy
	}

	t.Run("malformed tags are refused before the service", func(t *testing.T) {
		registry, spy := newSpyRegistry(t)

		result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_list_metrics", map[string]interface{}{
			"tags": "{not-valid-json",
		})
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid JSON")
		assert.Zero(t, spy.calls, "ListMetrics must not be called for malformed tags")
	})

	t.Run("well-formed tags reach the service once", func(t *testing.T) {
		registry, spy := newSpyRegistry(t)

		result, err := registry.ExecuteTool(context.Background(), "qrdb_ts_list_metrics", map[string]interface{}{
			"tags": `{"host":"server-1"}`,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, 1, spy.calls)
	})
}

func TestIsTimeSeriesTool(t *testing.T) {
	tsTools := []string{
		"qrdb_ts_write_point", "qrdb_ts_write_batch",
		"qrdb_ts_query", "qrdb_ts_delete_series",
		"qrdb_ts_get_retention_policy", "qrdb_ts_set_retention_policy",
		"qrdb_ts_list_metrics",
	}
	for _, name := range tsTools {
		assert.True(t, isTimeSeriesTool(name))
	}
	assert.False(t, isTimeSeriesTool("qrdb_put"))
	assert.False(t, isTimeSeriesTool(""))
}

func TestTimeSeriesErrorTypes(t *testing.T) {
	t.Run("required field error", func(t *testing.T) {
		err := &TimeSeriesRequiredFieldError{Field: "metric"}
		assert.Equal(t, "timeseries: metric is required", err.Error())
	})

	t.Run("parse error", func(t *testing.T) {
		err := &TimeSeriesParseError{Field: "value", Message: "not a number"}
		assert.Equal(t, "timeseries parse error: value: not a number", err.Error())
	})
}

func TestGetInt64(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected int64
	}{
		{"float64", map[string]interface{}{"v": float64(42)}, "v", 42},
		{"float64 zero", map[string]interface{}{"v": float64(0)}, "v", 0},
		{"float64 large", map[string]interface{}{"v": float64(9999999999)}, "v", 9999999999},
		{"float64 negative", map[string]interface{}{"v": float64(-500)}, "v", -500},
		{"int", map[string]interface{}{"v": int(10)}, "v", 10},
		{"int64", map[string]interface{}{"v": int64(100)}, "v", 100},
		{"uint64", map[string]interface{}{"v": uint64(200)}, "v", 200},
		{"numeric string", map[string]interface{}{"v": "999"}, "v", 999},
		{"non-numeric string", map[string]interface{}{"v": "string"}, "v", 0},
		{"empty string", map[string]interface{}{"v": ""}, "v", 0},
		{"missing", map[string]interface{}{}, "v", 0},
		{"nil", map[string]interface{}{"v": nil}, "v", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, getInt64(tc.args, tc.key))
		})
	}
}

func TestParseTagsJSON(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantNil bool
		wantErr bool
	}{
		{"no tags", map[string]interface{}{}, true, false},
		{"valid tags", map[string]interface{}{"tags": `{"host":"n1"}`}, false, false},
		{"invalid JSON", map[string]interface{}{"tags": "bad"}, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseTagsJSON(tc.args)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.wantNil {
				assert.Nil(t, result)
			}
		})
	}
}

// =============================================================================
// Scheduler Job History Tests - with executions
// =============================================================================

func TestSchedulerJobHistory_WithExecutions(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			historyFunc: func(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
				return &service.GetSchedulerJobHistoryResponse{
					Executions: []*service.SchedulerJobExecution{
						{
							ID:        1,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusCompleted,
							StartedAt: time.Now().Add(-time.Hour),
							EndedAt:   time.Now().Add(-30 * time.Minute),
						},
						{
							ID:        2,
							JobID:     req.JobID,
							Status:    service.SchedulerJobStatusFailed,
							StartedAt: time.Now().Add(-2 * time.Hour),
							EndedAt:   time.Now().Add(-90 * time.Minute),
							Error:     "timeout exceeded",
						},
					},
					HasMore:    true,
					NextCursor: "cursor-456",
					Timestamp:  time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_job_history", map[string]interface{}{
		"job_id": float64(123),
		"limit":  float64(10),
		"cursor": "prev-cursor",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["has_more"])
	assert.Equal(t, "cursor-456", resultMap["next_cursor"])
}

// =============================================================================
// Scheduler Status with Stats Tests
// =============================================================================

func TestSchedulerStatus_WithStats(t *testing.T) {
	svc := &extendedMockService{
		schedulerSvc: &mockSchedulerService{
			statusFunc: func(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
				return &service.GetSchedulerStatusResponse{
					Running: true,
					Stats: &service.SchedulerStats{
						JobCount:     100,
						RunningJobs:  10,
						SuccessCount: 85,
						FailureCount: 5,
						StartedAt:    time.Now().Add(-24 * time.Hour),
					},
					Timestamp: time.Now(),
				}, nil
			},
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	result, err := registry.ExecuteTool(ctx, "quicbaas_scheduler_status", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["running"])
}

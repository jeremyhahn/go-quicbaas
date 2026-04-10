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

// Package cli provides tests for output formatting.
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestParseOutputFormat tests output format parsing.
func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    OutputFormat
		wantErr bool
	}{
		{
			name:    "json lowercase",
			input:   "json",
			want:    OutputFormatJSON,
			wantErr: false,
		},
		{
			name:    "json uppercase",
			input:   "JSON",
			want:    OutputFormatJSON,
			wantErr: false,
		},
		{
			name:    "yaml lowercase",
			input:   "yaml",
			want:    OutputFormatYAML,
			wantErr: false,
		},
		{
			name:    "yml variant",
			input:   "yml",
			want:    OutputFormatYAML,
			wantErr: false,
		},
		{
			name:    "table",
			input:   "table",
			want:    OutputFormatTable,
			wantErr: false,
		},
		{
			name:    "raw",
			input:   "raw",
			want:    OutputFormatRaw,
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOutputFormat(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unknown output format")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestFormatPutResponse tests Put response formatting.
func TestFormatPutResponse(t *testing.T) {
	now := time.Now()
	resp := &PutResponse{
		Key:       "testkey",
		Success:   true,
		Message:   "stored successfully",
		Timestamp: now,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result PutResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.Key, result.Key)
		assert.Equal(t, resp.Success, result.Success)
	})

	t.Run("YAML format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result PutResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.Key, result.Key)
		assert.Equal(t, resp.Success, result.Success)
	})

	t.Run("Table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "testkey")
		assert.Contains(t, output, "true")
	})

	t.Run("Default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Successfully stored")
		assert.Contains(t, output, "testkey")
	})
}

// TestFormatGetResponse tests Get response formatting.
func TestFormatGetResponse(t *testing.T) {
	now := time.Now()

	t.Run("Found response", func(t *testing.T) {
		resp := &GetResponse{
			Key:       "testkey",
			Value:     []byte(`{"name":"test"}`),
			Found:     true,
			Timestamp: now,
		}

		formatter := NewFormatter(false)

		t.Run("JSON format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatGetResponse(resp, OutputFormatJSON, &buf)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)
			assert.True(t, result["found"].(bool))
		})

		t.Run("Table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatGetResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)

			output := buf.String()
			assert.Contains(t, output, "testkey")
			assert.Contains(t, output, "true")
			assert.Contains(t, output, "bytes")
		})

		t.Run("Raw format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatGetResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)

			assert.Equal(t, resp.Value, buf.Bytes())
		})
	})

	t.Run("Not found response", func(t *testing.T) {
		resp := &GetResponse{
			Key:   "missing",
			Found: false,
		}

		formatter := NewFormatter(false)

		var buf bytes.Buffer
		err := formatter.FormatGetResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "not found")
	})
}

// TestFormatDeleteResponse tests Delete response formatting.
func TestFormatDeleteResponse(t *testing.T) {
	now := time.Now()
	resp := &DeleteResponse{
		Key:       "testkey",
		Deleted:   true,
		Message:   "deleted successfully",
		Timestamp: now,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatDeleteResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result DeleteResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.Key, result.Key)
		assert.True(t, result.Deleted)
	})

	t.Run("Table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatDeleteResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "testkey")
		assert.Contains(t, output, "true")
	})
}

// TestFormatListResponse tests List response formatting.
func TestFormatListResponse(t *testing.T) {
	now := time.Now()
	resp := &ListResponse{
		Items: []ListItem{
			{Key: "key1", Value: []byte("value1"), Timestamp: now},
			{Key: "key2", Value: []byte("value2"), Timestamp: now},
		},
		NextCursor: "cursor123",
		Total:      2,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["items"])
	})

	t.Run("Table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
		assert.Contains(t, output, "key2")
		assert.Contains(t, output, "Total")
		assert.Contains(t, output, "cursor123")
	})

	t.Run("Default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
		assert.Contains(t, output, "key2")
		assert.Contains(t, output, "cursor123")
	})
}

// TestFormatQueryResponse tests Query response formatting.
func TestFormatQueryResponse(t *testing.T) {
	now := time.Now()
	resp := &QueryResponse{
		Items: []QueryItem{
			{
				Key:       "key1",
				Value:     []byte("value1"),
				IndexedBy: map[string]interface{}{"email": "test@example.com"},
				Timestamp: now,
			},
		},
		Total: 1,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.NotNil(t, result["items"])
	})

	t.Run("Table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
		assert.Contains(t, output, "email")
	})
}

// TestFormatClusterStatus tests cluster status formatting.
func TestFormatClusterStatus(t *testing.T) {
	now := time.Now()
	resp := &ClusterStatusResponse{
		Healthy:    true,
		LeaderID:   1,
		NodeCount:  3,
		ReadyNodes: 3,
		Nodes: []NodeInfo{
			{
				NodeID:    1,
				Address:   "node1:9000",
				IsLeader:  true,
				IsHealthy: true,
				LastSeen:  now,
			},
			{
				NodeID:    2,
				Address:   "node2:9000",
				IsLeader:  false,
				IsHealthy: true,
				LastSeen:  now,
			},
		},
		RaftGroupInfo: []RaftGroupInfo{
			{
				GroupID:      1,
				LeaderID:     1,
				MemberCount:  3,
				LogIndex:     100,
				CommitIndex:  100,
				AppliedIndex: 100,
			},
		},
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.True(t, result["healthy"].(bool))
	})

	t.Run("Table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "CLUSTER STATUS")
		assert.Contains(t, output, "node1:9000")
		assert.Contains(t, output, "RAFT GROUPS")
	})

	t.Run("Default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "healthy")
	})
}

// TestFormatError tests error formatting.
func TestFormatError(t *testing.T) {
	formatter := NewFormatter(false)
	err := assert.AnError

	var buf bytes.Buffer
	fmtErr := formatter.FormatError(err, &buf)
	require.NoError(t, fmtErr)

	output := buf.String()
	assert.Contains(t, output, "Error:")
}

// TestColorOutput tests colored vs non-colored output.
func TestColorOutput(t *testing.T) {
	t.Run("without color", func(t *testing.T) {
		formatter := NewFormatter(false)
		err := assert.AnError

		var buf bytes.Buffer
		formatter.FormatError(err, &buf)

		output := buf.String()
		assert.NotContains(t, output, "\033[") // No ANSI color codes
	})

	t.Run("with color", func(t *testing.T) {
		formatter := NewFormatter(true)
		err := assert.AnError

		var buf bytes.Buffer
		formatter.FormatError(err, &buf)

		output := buf.String()
		assert.Contains(t, output, "\033[") // Has ANSI color codes
	})
}

// TestEmptyResponses tests formatting of empty responses.
func TestEmptyResponses(t *testing.T) {
	formatter := NewFormatter(false)

	t.Run("empty list", func(t *testing.T) {
		resp := &ListResponse{
			Items: []ListItem{},
			Total: 0,
		}

		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Total")
		assert.Contains(t, output, "0")
	})

	t.Run("empty query", func(t *testing.T) {
		resp := &QueryResponse{
			Items: []QueryItem{},
			Total: 0,
		}

		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Total")
	})
}

// TestLargeDatasets tests formatting with large datasets.
func TestLargeDatasets(t *testing.T) {
	formatter := NewFormatter(false)
	now := time.Now()

	items := make([]ListItem, 1000)
	for i := 0; i < 1000; i++ {
		items[i] = ListItem{
			Key:       strings.Repeat("a", 100),
			Value:     []byte(strings.Repeat("b", 1000)),
			Timestamp: now,
		}
	}

	resp := &ListResponse{
		Items: items,
		Total: 1000,
	}

	t.Run("JSON format with large dataset", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)
		assert.Greater(t, buf.Len(), 0)
	})

	t.Run("Table format with large dataset", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		assert.Greater(t, buf.Len(), 0)
	})
}

// BenchmarkFormatJSON benchmarks JSON formatting.
func BenchmarkFormatJSON(b *testing.B) {
	formatter := &DefaultFormatter{}
	resp := &PutResponse{
		Key:       "testkey",
		Success:   true,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter.FormatPutResponse(resp, OutputFormatJSON, &buf)
	}
}

// BenchmarkFormatYAML benchmarks YAML formatting.
func BenchmarkFormatYAML(b *testing.B) {
	formatter := &DefaultFormatter{}
	resp := &PutResponse{
		Key:       "testkey",
		Success:   true,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter.FormatPutResponse(resp, OutputFormatYAML, &buf)
	}
}

// BenchmarkFormatTable benchmarks table formatting.
func BenchmarkFormatTable(b *testing.B) {
	formatter := &DefaultFormatter{}
	resp := &PutResponse{
		Key:       "testkey",
		Success:   true,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter.FormatPutResponse(resp, OutputFormatTable, &buf)
	}
}

// BenchmarkParseOutputFormat benchmarks format parsing.
func BenchmarkParseOutputFormat(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseOutputFormat("json")
		ParseOutputFormat("yaml")
		ParseOutputFormat("table")
	}
}

// BenchmarkFormatListLarge benchmarks list formatting with large dataset.
func BenchmarkFormatListLarge(b *testing.B) {
	formatter := &DefaultFormatter{}
	now := time.Now()

	items := make([]ListItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = ListItem{
			Key:       "key",
			Value:     []byte("value"),
			Timestamp: now,
		}
	}

	resp := &ListResponse{
		Items: items,
		Total: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter.FormatListResponse(resp, OutputFormatTable, &buf)
	}
}

// Additional edge case tests for 90%+ coverage

func TestFormatPutResponse_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("failed put response", func(t *testing.T) {
		resp := &PutResponse{
			Key:       "failkey",
			Success:   false,
			Message:   "write failed",
			Timestamp: now,
		}

		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Failed")
		assert.Contains(t, output, "failkey")
	})

	t.Run("put response with empty message", func(t *testing.T) {
		resp := &PutResponse{
			Key:       "testkey",
			Success:   true,
			Message:   "",
			Timestamp: now,
		}

		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})
}

func TestFormatGetResponse_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("not found with table format", func(t *testing.T) {
		resp := &GetResponse{
			Key:   "missing",
			Found: false,
		}

		var buf bytes.Buffer
		err := formatter.FormatGetResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "false")
	})

	t.Run("raw format not found", func(t *testing.T) {
		resp := &GetResponse{
			Key:   "missing",
			Found: false,
		}

		var buf bytes.Buffer
		err := formatter.FormatGetResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("yaml format found", func(t *testing.T) {
		resp := &GetResponse{
			Key:       "testkey",
			Value:     []byte("testvalue"),
			Found:     true,
			Timestamp: now,
		}

		var buf bytes.Buffer
		err := formatter.FormatGetResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "testkey")
		assert.Contains(t, output, "true")
	})
}

func TestFormatDeleteResponse_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("failed delete", func(t *testing.T) {
		resp := &DeleteResponse{
			Key:       "testkey",
			Deleted:   false,
			Message:   "key not found",
			Timestamp: now,
		}

		var buf bytes.Buffer
		err := formatter.FormatDeleteResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Failed")
		assert.Contains(t, output, "testkey")
	})

	t.Run("yaml format", func(t *testing.T) {
		resp := &DeleteResponse{
			Key:       "testkey",
			Deleted:   true,
			Timestamp: now,
		}

		var buf bytes.Buffer
		err := formatter.FormatDeleteResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})
}

func TestFormatListResponse_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("no cursor", func(t *testing.T) {
		resp := &ListResponse{
			Items: []ListItem{
				{Key: "key1", Value: []byte("value1"), Timestamp: now},
			},
			NextCursor: "",
			Total:      1,
		}

		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
		assert.NotContains(t, output, "cursor")
	})

	t.Run("yaml format", func(t *testing.T) {
		resp := &ListResponse{
			Items:      []ListItem{},
			NextCursor: "abc123",
			Total:      0,
		}

		var buf bytes.Buffer
		err := formatter.FormatListResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})
}

func TestFormatQueryResponse_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("empty indexed fields", func(t *testing.T) {
		resp := &QueryResponse{
			Items: []QueryItem{
				{
					Key:       "key1",
					Value:     []byte("value1"),
					IndexedBy: map[string]interface{}{},
					Timestamp: now,
				},
			},
			Total: 1,
		}

		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})

	t.Run("multiple indexed fields", func(t *testing.T) {
		resp := &QueryResponse{
			Items: []QueryItem{
				{
					Key:   "key1",
					Value: []byte("value1"),
					IndexedBy: map[string]interface{}{
						"email": "test@example.com",
						"age":   30,
						"name":  "John",
					},
					Timestamp: now,
				},
			},
			Total: 1,
		}

		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
	})

	t.Run("default format", func(t *testing.T) {
		resp := &QueryResponse{
			Items: []QueryItem{
				{
					Key:       "key1",
					Value:     []byte("value1"),
					IndexedBy: map[string]interface{}{"field": "value"},
					Timestamp: now,
				},
			},
			Total: 1,
		}

		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "key1")
		assert.Contains(t, output, "Total:")
	})

	t.Run("yaml format", func(t *testing.T) {
		resp := &QueryResponse{
			Items: []QueryItem{},
			Total: 0,
		}

		var buf bytes.Buffer
		err := formatter.FormatQueryResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})
}

func TestFormatClusterStatus_EdgeCases(t *testing.T) {
	now := time.Now()
	formatter := NewFormatter(false)

	t.Run("unhealthy cluster", func(t *testing.T) {
		resp := &ClusterStatusResponse{
			Healthy:    false,
			LeaderID:   0,
			NodeCount:  3,
			ReadyNodes: 1,
			Nodes:      []NodeInfo{},
		}

		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, "", &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "unhealthy")
	})

	t.Run("empty nodes and groups", func(t *testing.T) {
		resp := &ClusterStatusResponse{
			Healthy:       true,
			LeaderID:      1,
			NodeCount:     3,
			ReadyNodes:    3,
			Nodes:         []NodeInfo{},
			RaftGroupInfo: []RaftGroupInfo{},
		}

		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "CLUSTER STATUS")
	})

	t.Run("yaml format", func(t *testing.T) {
		resp := &ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  1,
			ReadyNodes: 1,
		}

		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})

	t.Run("multiple raft groups", func(t *testing.T) {
		resp := &ClusterStatusResponse{
			Healthy:    true,
			LeaderID:   1,
			NodeCount:  3,
			ReadyNodes: 3,
			Nodes: []NodeInfo{
				{NodeID: 1, Address: "node1:9000", IsLeader: true, IsHealthy: true, LastSeen: now},
			},
			RaftGroupInfo: []RaftGroupInfo{
				{GroupID: 1, LeaderID: 1, MemberCount: 3, LogIndex: 100, CommitIndex: 100, AppliedIndex: 100},
				2: {GroupID: 2, LeaderID: 1, MemberCount: 3, LogIndex: 50, CommitIndex: 50, AppliedIndex: 50},
			},
		}

		var buf bytes.Buffer
		err := formatter.FormatClusterStatus(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "RAFT GROUPS")
	})
}

func TestParseOutputFormat_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  OutputFormat
	}{
		{"JSON", OutputFormatJSON},
		{"Json", OutputFormatJSON},
		{"YAML", OutputFormatYAML},
		{"Yaml", OutputFormatYAML},
		{"YML", OutputFormatYAML},
		{"TABLE", OutputFormatTable},
		{"Table", OutputFormatTable},
		{"RAW", OutputFormatRaw},
		{"Raw", OutputFormatRaw},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseOutputFormat(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatError_WithColor(t *testing.T) {
	formatter := NewFormatter(true)
	err := assert.AnError

	var buf bytes.Buffer
	fmtErr := formatter.FormatError(err, &buf)
	require.NoError(t, fmtErr)

	output := buf.String()
	// Should contain color codes
	assert.Contains(t, output, "\033[31m")
	assert.Contains(t, output, "\033[0m")
}

func TestFormatters_NilResponses(t *testing.T) {
	formatter := NewFormatter(false)

	t.Run("empty put message", func(t *testing.T) {
		resp := &PutResponse{
			Key:       "key",
			Success:   true,
			Message:   "",
			Timestamp: time.Now(),
		}

		var buf bytes.Buffer
		err := formatter.FormatPutResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
	})

	t.Run("empty delete message", func(t *testing.T) {
		resp := &DeleteResponse{
			Key:       "key",
			Deleted:   true,
			Message:   "",
			Timestamp: time.Now(),
		}

		var buf bytes.Buffer
		err := formatter.FormatDeleteResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
	})
}

// =============================================================================
// Consolidated from output_additional_test.go
// =============================================================================

// TestFormatBackupCreateResponse tests backup create response formatting.
func TestFormatBackupCreateResponse(t *testing.T) {
	now := time.Now()
	resp := &BackupCreateResponse{
		BackupID:    "backup-123",
		Type:        "full",
		Size:        1024 * 1024 * 100, // 100MB
		Compression: "gzip",
		Encryption:  "aes256",
		Duration:    5 * time.Second,
		Timestamp:   now,
		Success:     true,
		Message:     "Backup completed",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupCreateResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result BackupCreateResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
		assert.Equal(t, resp.Type, result.Type)
	})

	t.Run("YAML format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupCreateResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result BackupCreateResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupCreateResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "BACKUP CREATED")
		assert.Contains(t, output, "backup-123")
		assert.Contains(t, output, "full")
		assert.Contains(t, output, "gzip")
		assert.Contains(t, output, "aes256")
	})

	t.Run("default format - success", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupCreateResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Backup created successfully")
		assert.Contains(t, output, "backup-123")
	})

	t.Run("default format - failure", func(t *testing.T) {
		failedResp := &BackupCreateResponse{
			BackupID: "backup-456",
			Success:  false,
			Message:  "Backup failed",
		}
		var buf bytes.Buffer
		err := formatter.FormatBackupCreateResponse(failedResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Backup failed")
	})
}

// TestFormatBackupListResponse tests backup list response formatting.
func TestFormatBackupListResponse(t *testing.T) {
	now := time.Now()
	resp := &BackupListResponse{
		Backups: []BackupInfo{
			{
				ID:          "backup-1",
				Type:        "full",
				Size:        1024 * 1024,
				Compression: "gzip",
				Encryption:  "aes256",
				NumShards:   3,
				NumKeys:     1000,
				Timestamp:   now,
			},
			{
				ID:          "backup-2",
				Type:        "incremental",
				Size:        512 * 1024,
				Compression: "zstd",
				Encryption:  "none",
				NumShards:   3,
				NumKeys:     500,
				Timestamp:   now.Add(-1 * time.Hour),
			},
		},
		TotalCount:  2,
		StoragePath: "/var/backups",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupListResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result BackupListResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Len(t, result.Backups, 2)
		assert.Equal(t, 2, result.TotalCount)
	})

	t.Run("YAML format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupListResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result BackupListResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Len(t, result.Backups, 2)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupListResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "BACKUPS")
		assert.Contains(t, output, "backup-1")
		assert.Contains(t, output, "backup-2")
		assert.Contains(t, output, "Total backups")
		assert.Contains(t, output, "/var/backups")
	})

	t.Run("default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupListResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "backup-1")
		assert.Contains(t, output, "backup-2")
		assert.Contains(t, output, "Total: 2 backups")
	})
}

// TestFormatBackupRestoreResponse tests backup restore response formatting.
func TestFormatBackupRestoreResponse(t *testing.T) {
	now := time.Now()
	resp := &BackupRestoreResponse{
		BackupID:       "backup-123",
		KeysRestored:   1000,
		ShardsRestored: 3,
		Duration:       10 * time.Second,
		Timestamp:      now,
		Success:        true,
		Message:        "Restore completed",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupRestoreResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result BackupRestoreResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
		assert.Equal(t, int64(1000), result.KeysRestored)
	})

	t.Run("YAML format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupRestoreResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result BackupRestoreResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupRestoreResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "RESTORE COMPLETE")
		assert.Contains(t, output, "backup-123")
		assert.Contains(t, output, "1000")
		assert.Contains(t, output, "3")
	})

	t.Run("default format - success", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupRestoreResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Restore completed successfully")
		assert.Contains(t, output, "1000 keys")
		assert.Contains(t, output, "3 shards")
	})

	t.Run("default format - failure", func(t *testing.T) {
		failedResp := &BackupRestoreResponse{
			BackupID: "backup-456",
			Success:  false,
			Message:  "Restore failed",
		}
		var buf bytes.Buffer
		err := formatter.FormatBackupRestoreResponse(failedResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Restore failed")
	})
}

// TestFormatBackupDeleteResponse tests backup delete response formatting.
func TestFormatBackupDeleteResponse(t *testing.T) {
	now := time.Now()
	resp := &BackupDeleteResponse{
		BackupID:  "backup-123",
		Deleted:   true,
		Timestamp: now,
		Message:   "Backup deleted",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupDeleteResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result BackupDeleteResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
		assert.True(t, result.Deleted)
	})

	t.Run("YAML format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupDeleteResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result BackupDeleteResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, resp.BackupID, result.BackupID)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupDeleteResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "backup-123")
		assert.Contains(t, output, "true")
	})

	t.Run("default format - success", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatBackupDeleteResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Successfully deleted backup")
		assert.Contains(t, output, "backup-123")
	})

	t.Run("default format - failure", func(t *testing.T) {
		failedResp := &BackupDeleteResponse{
			BackupID: "backup-456",
			Deleted:  false,
			Message:  "Delete failed",
		}
		var buf bytes.Buffer
		err := formatter.FormatBackupDeleteResponse(failedResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Failed to delete backup")
		assert.Contains(t, output, "Delete failed")
	})
}

// TestFormatBackupVerifyResponse tests backup verify response formatting.
func TestFormatBackupVerifyResponse(t *testing.T) {
	now := time.Now()

	t.Run("valid backup", func(t *testing.T) {
		resp := &BackupVerifyResponse{
			BackupID:      "backup-123",
			Valid:         true,
			ChecksumValid: true,
			ShardsValid:   3,
			Issues:        []string{},
			Timestamp:     now,
		}

		formatter := NewFormatter(false)

		var buf bytes.Buffer
		err := formatter.FormatBackupVerifyResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "BACKUP VERIFICATION")
		assert.Contains(t, output, "backup-123")
		assert.Contains(t, output, "true")
		assert.Contains(t, output, "None")
	})

	t.Run("invalid backup with issues", func(t *testing.T) {
		resp := &BackupVerifyResponse{
			BackupID:      "backup-456",
			Valid:         false,
			ChecksumValid: false,
			ShardsValid:   2,
			Issues:        []string{"checksum mismatch", "missing shard"},
			Timestamp:     now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupVerifyResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "backup-456")
			assert.Contains(t, output, "checksum mismatch")
			assert.Contains(t, output, "missing shard")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupVerifyResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "verification failed")
			assert.Contains(t, output, "checksum mismatch")
		})
	})

	t.Run("JSON format", func(t *testing.T) {
		resp := &BackupVerifyResponse{
			BackupID: "backup-789",
			Valid:    true,
		}

		formatter := NewFormatter(false)
		var buf bytes.Buffer
		err := formatter.FormatBackupVerifyResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result BackupVerifyResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "backup-789", result.BackupID)
	})

	t.Run("default format - valid", func(t *testing.T) {
		resp := &BackupVerifyResponse{
			BackupID:    "backup-valid",
			Valid:       true,
			ShardsValid: 5,
		}

		formatter := NewFormatter(false)
		var buf bytes.Buffer
		err := formatter.FormatBackupVerifyResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Backup is valid")
		assert.Contains(t, output, "5 shards valid")
	})
}

// TestFormatBackupStatusResponse tests backup status response formatting.
func TestFormatBackupStatusResponse(t *testing.T) {
	now := time.Now()

	t.Run("idle state", func(t *testing.T) {
		resp := &BackupStatusResponse{
			State:     "idle",
			Message:   "No backup operations in progress",
			Timestamp: now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "BACKUP STATUS")
			assert.Contains(t, output, "idle")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "No backup operations in progress")
		})
	})

	t.Run("running state", func(t *testing.T) {
		resp := &BackupStatusResponse{
			BackupID:       "backup-123",
			State:          "running",
			Progress:       50,
			ShardsTotal:    6,
			ShardsComplete: 3,
			BytesTotal:     1024 * 1024 * 100,
			BytesComplete:  1024 * 1024 * 50,
			StartTime:      now.Add(-5 * time.Minute),
			ETA:            5 * time.Minute,
			Timestamp:      now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "BACKUP STATUS")
			assert.Contains(t, output, "backup-123")
			assert.Contains(t, output, "running")
			assert.Contains(t, output, "50%")
			assert.Contains(t, output, "3 / 6")
			assert.Contains(t, output, "ETA")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "Backup in progress")
			assert.Contains(t, output, "50%")
			assert.Contains(t, output, "3/6 shards")
		})

		t.Run("JSON format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatJSON, &buf)
			require.NoError(t, err)

			var result BackupStatusResponse
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)
			assert.Equal(t, "backup-123", result.BackupID)
			assert.Equal(t, "running", result.State)
			assert.Equal(t, 50, result.Progress)
		})
	})

	t.Run("completed state - valid", func(t *testing.T) {
		resp := &BackupStatusResponse{
			BackupID:       "backup-456",
			State:          "completed",
			Progress:       100,
			ShardsTotal:    6,
			ShardsComplete: 6,
			BytesTotal:     1024 * 1024 * 100,
			BytesComplete:  1024 * 1024 * 100,
			Valid:          true,
			ChecksumValid:  true,
			Timestamp:      now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "BACKUP STATUS")
			assert.Contains(t, output, "backup-456")
			assert.Contains(t, output, "completed")
			assert.Contains(t, output, "6")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "Backup completed")
			assert.Contains(t, output, "Valid")
		})
	})

	t.Run("completed state - invalid with issues", func(t *testing.T) {
		resp := &BackupStatusResponse{
			BackupID:      "backup-789",
			State:         "completed",
			Progress:      100,
			ShardsTotal:   6,
			Valid:         false,
			ChecksumValid: false,
			Issues:        []string{"checksum mismatch in shard 3", "corrupted data block"},
			Timestamp:     now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "backup-789")
			assert.Contains(t, output, "Issues")
			assert.Contains(t, output, "checksum mismatch")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "Invalid")
			assert.Contains(t, output, "checksum mismatch")
		})
	})

	t.Run("failed state", func(t *testing.T) {
		resp := &BackupStatusResponse{
			BackupID:  "backup-failed",
			State:     "failed",
			Message:   "Disk full error",
			Timestamp: now,
		}

		formatter := NewFormatter(false)

		t.Run("table format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatTable, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "BACKUP STATUS")
			assert.Contains(t, output, "failed")
			assert.Contains(t, output, "Disk full error")
		})

		t.Run("default format", func(t *testing.T) {
			var buf bytes.Buffer
			err := formatter.FormatBackupStatusResponse(resp, OutputFormatRaw, &buf)
			require.NoError(t, err)
			output := buf.String()
			assert.Contains(t, output, "Backup failed")
			assert.Contains(t, output, "Disk full error")
		})
	})

	t.Run("YAML format", func(t *testing.T) {
		resp := &BackupStatusResponse{
			BackupID:  "backup-yaml",
			State:     "completed",
			Progress:  100,
			Valid:     true,
			Timestamp: now,
		}

		formatter := NewFormatter(false)
		var buf bytes.Buffer
		err := formatter.FormatBackupStatusResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result BackupStatusResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "backup-yaml", result.BackupID)
		assert.Equal(t, "completed", result.State)
	})
}

// TestFormatBytes tests byte formatting.
func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"small bytes", 512, "512 B"},
		{"1 KB", 1024, "1.0 KB"},
		{"1.5 KB", 1536, "1.5 KB"},
		{"1 MB", 1024 * 1024, "1.0 MB"},
		{"100 MB", 100 * 1024 * 1024, "100.0 MB"},
		{"1 GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"1.5 GB", 1536 * 1024 * 1024, "1.5 GB"},
		{"1 TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFormatCacheStatsResponse tests cache stats response formatting.
func TestFormatCacheStatsResponse(t *testing.T) {
	resp := &CacheStatsResponse{
		EntityType: "user",
		Enabled:    true,
		Stats: CacheStats{
			Enabled:    true,
			L1Size:     500,
			L1Capacity: 1000,
			L1Hits:     8000,
			L1Misses:   2000,
			L1HitRate:  0.8,
		},
		ShowDetails: true,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheStatsResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheStatsResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "user", result.EntityType)
		assert.True(t, result.Enabled)
	})

	t.Run("table format - enabled", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheStatsResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "CACHE STATISTICS: user")
		assert.Contains(t, output, "500 / 1000")
		assert.Contains(t, output, "80.00%")
	})

	t.Run("table format - disabled", func(t *testing.T) {
		disabledResp := &CacheStatsResponse{
			EntityType: "product",
			Enabled:    false,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheStatsResponse(disabledResp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Cache disabled for this entity")
	})

	t.Run("default format - enabled", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheStatsResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "80.00%")
	})

	t.Run("default format - disabled", func(t *testing.T) {
		disabledResp := &CacheStatsResponse{
			EntityType: "product",
			Enabled:    false,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheStatsResponse(disabledResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Cache disabled")
	})
}

// TestFormatCacheAllStatsResponse tests all cache stats formatting.
func TestFormatCacheAllStatsResponse(t *testing.T) {
	resp := &CacheAllStatsResponse{
		EntityStats: map[string]CacheEntityStats{
			"user": {
				EntityType: "user",
				Enabled:    true,
				Stats: CacheStats{
					L1Size:     500,
					L1Capacity: 1000,
					L1Hits:     8000,
					L1Misses:   2000,
					L1HitRate:  0.8,
				},
			},
			"product": {
				EntityType: "product",
				Enabled:    false,
			},
		},
		ShowDetails: true,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllStatsResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheAllStatsResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Len(t, result.EntityStats, 2)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllStatsResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "CACHE STATISTICS (ALL ENTITIES)")
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "product")
		assert.Contains(t, output, "disabled")
	})

	t.Run("default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllStatsResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "user:")
		assert.Contains(t, output, "product: disabled")
	})
}

// TestFormatCacheClearResponse tests cache clear response formatting.
func TestFormatCacheClearResponse(t *testing.T) {
	now := time.Now()

	t.Run("clear specific entity", func(t *testing.T) {
		resp := &CacheClearResponse{
			EntityType:     "user",
			EntriesCleared: 500,
			Duration:       100 * time.Millisecond,
			Timestamp:      now,
			Success:        true,
			Message:        "Cache cleared",
		}

		formatter := NewFormatter(false)

		var buf bytes.Buffer
		err := formatter.FormatCacheClearResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "CACHE CLEARED")
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "500")
	})

	t.Run("clear all entities", func(t *testing.T) {
		resp := &CacheClearResponse{
			EntityType:     "",
			EntriesCleared: 1500,
			Duration:       200 * time.Millisecond,
			Timestamp:      now,
			Success:        true,
		}

		formatter := NewFormatter(false)

		var buf bytes.Buffer
		err := formatter.FormatCacheClearResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "All entities")
	})

	t.Run("default format - success", func(t *testing.T) {
		resp := &CacheClearResponse{
			EntityType:     "user",
			EntriesCleared: 500,
			Success:        true,
		}

		formatter := NewFormatter(false)
		var buf bytes.Buffer
		err := formatter.FormatCacheClearResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Cleared 500 cache entries")
		assert.Contains(t, output, "user")
	})

	t.Run("default format - failure", func(t *testing.T) {
		resp := &CacheClearResponse{
			Success: false,
			Message: "Clear failed",
		}

		formatter := NewFormatter(false)
		var buf bytes.Buffer
		err := formatter.FormatCacheClearResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Failed to clear cache")
	})
}

// TestFormatCacheConfigResponse tests cache config response formatting.
func TestFormatCacheConfigResponse(t *testing.T) {
	resp := &CacheConfigResponse{
		EntityType: "user",
		Enabled:    true,
		TTL:        5 * time.Minute,
		MaxSize:    1000,
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheConfigResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheConfigResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "user", result.EntityType)
	})

	t.Run("table format - enabled", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheConfigResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "CACHE CONFIGURATION: user")
		assert.Contains(t, output, "5m0s")
		assert.Contains(t, output, "1000 entries")
	})

	t.Run("table format - proportional allocation", func(t *testing.T) {
		resp := &CacheConfigResponse{
			EntityType: "user",
			Enabled:    true,
			TTL:        5 * time.Minute,
			MaxSize:    0, // Proportional
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheConfigResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Proportional allocation")
	})

	t.Run("default format - enabled", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheConfigResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "true")
	})

	t.Run("default format - disabled", func(t *testing.T) {
		disabledResp := &CacheConfigResponse{
			EntityType: "product",
			Enabled:    false,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheConfigResponse(disabledResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "product")
		assert.Contains(t, output, "false")
	})
}

// TestFormatCacheAllConfigResponse tests all cache config formatting.
func TestFormatCacheAllConfigResponse(t *testing.T) {
	resp := &CacheAllConfigResponse{
		GlobalEnabled:   true,
		DefaultTTL:      10 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		EntityConfigs: map[string]CacheEntityConfig{
			"user": {
				EntityType: "user",
				Enabled:    true,
				TTL:        5 * time.Minute,
				MaxSize:    1000,
			},
			"product": {
				EntityType: "product",
				Enabled:    true,
				TTL:        15 * time.Minute,
				MaxSize:    0, // Proportional
			},
		},
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllConfigResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheAllConfigResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.True(t, result.GlobalEnabled)
		assert.Len(t, result.EntityConfigs, 2)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllConfigResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "CACHE CONFIGURATION")
		assert.Contains(t, output, "GLOBAL SETTINGS")
		assert.Contains(t, output, "ENTITY-SPECIFIC SETTINGS")
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "product")
		assert.Contains(t, output, "proportional")
	})

	t.Run("table format - no entity configs", func(t *testing.T) {
		emptyResp := &CacheAllConfigResponse{
			GlobalEnabled:   true,
			DefaultTTL:      10 * time.Minute,
			MaxSize:         10000,
			CleanupInterval: 1 * time.Minute,
			EntityConfigs:   map[string]CacheEntityConfig{},
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheAllConfigResponse(emptyResp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "No entity-specific configurations")
	})

	t.Run("default format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheAllConfigResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Global: Enabled=true")
		assert.Contains(t, output, "2")
	})
}

// TestFormatCacheEnableResponse tests cache enable response formatting.
func TestFormatCacheEnableResponse(t *testing.T) {
	now := time.Now()
	resp := &CacheEnableResponse{
		EntityType: "user",
		Enabled:    true,
		Timestamp:  now,
		Success:    true,
		Message:    "Cache enabled for user",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheEnableResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheEnableResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "user", result.EntityType)
		assert.True(t, result.Enabled)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheEnableResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "true")
	})

	t.Run("default format - success", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheEnableResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Cache enabled for user")
	})

	t.Run("default format - failure", func(t *testing.T) {
		failedResp := &CacheEnableResponse{
			Success: false,
			Message: "Enable failed",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheEnableResponse(failedResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Failed to enable cache")
	})
}

// TestFormatCacheDisableResponse tests cache disable response formatting.
func TestFormatCacheDisableResponse(t *testing.T) {
	now := time.Now()
	resp := &CacheDisableResponse{
		EntityType: "user",
		Enabled:    false,
		Timestamp:  now,
		Success:    true,
		Message:    "Cache disabled for user",
	}

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheDisableResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheDisableResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "user", result.EntityType)
		assert.False(t, result.Enabled)
	})

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheDisableResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "user")
		assert.Contains(t, output, "false")
	})

	t.Run("default format - success", func(t *testing.T) {
		var buf bytes.Buffer
		err := formatter.FormatCacheDisableResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Cache disabled for user")
	})

	t.Run("default format - failure", func(t *testing.T) {
		failedResp := &CacheDisableResponse{
			Success: false,
			Message: "Disable failed",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheDisableResponse(failedResp, OutputFormatRaw, &buf)
		require.NoError(t, err)
		output := buf.String()
		assert.Contains(t, output, "Failed to disable cache")
	})
}

// BenchmarkFormatBackupCreateResponse benchmarks backup create formatting.
func BenchmarkFormatBackupCreateResponse(b *testing.B) {
	resp := &BackupCreateResponse{
		BackupID:  "backup-123",
		Type:      "full",
		Size:      1024 * 1024 * 100,
		Success:   true,
		Timestamp: time.Now(),
	}
	formatter := NewFormatter(false)
	var buf bytes.Buffer

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		formatter.FormatBackupCreateResponse(resp, OutputFormatJSON, &buf)
	}
}

// BenchmarkFormatBytes benchmarks byte formatting.
func BenchmarkFormatBytes(b *testing.B) {
	size := int64(1024 * 1024 * 1024) // 1GB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatBytes(size)
	}
}

// =============================================================================
// Consolidated from output_coverage_test.go
// =============================================================================

// TestFormatCacheWarmResponse tests cache warming response formatting.
func TestFormatCacheWarmResponse(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(5 * time.Minute)

	formatter := NewFormatter(false)

	t.Run("JSON format", func(t *testing.T) {
		resp := &CacheWarmResponse{
			Prefixes:      []string{"users/", "sessions/"},
			State:         "completed",
			StartedAt:     now,
			CompletedAt:   completedAt,
			KeysWarmed:    1000,
			KeysFailed:    5,
			CurrentPrefix: "",
			Duration:      5 * time.Minute,
			Timestamp:     now,
			Success:       true,
			Message:       "Cache warming completed",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatJSON, &buf)
		require.NoError(t, err)

		var result CacheWarmResponse
		err = json.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "completed", result.State)
		assert.Equal(t, int64(1000), result.KeysWarmed)
		assert.Len(t, result.Prefixes, 2)
	})

	t.Run("YAML format", func(t *testing.T) {
		resp := &CacheWarmResponse{
			Prefixes:   []string{"entities/"},
			State:      "warming",
			StartedAt:  now,
			KeysWarmed: 500,
			Timestamp:  now,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatYAML, &buf)
		require.NoError(t, err)

		var result CacheWarmResponse
		err = yaml.Unmarshal(buf.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, "warming", result.State)
	})

	t.Run("table format with prefixes", func(t *testing.T) {
		resp := &CacheWarmResponse{
			Prefixes:      []string{"users/", "sessions/", "tokens/"},
			State:         "warming",
			StartedAt:     now,
			KeysWarmed:    250,
			KeysFailed:    2,
			CurrentPrefix: "sessions/",
			Duration:      2 * time.Minute,
			Timestamp:     now,
			Success:       false,
			Message:       "In progress",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "CACHE WARMING")
		assert.Contains(t, output, "warming")
		assert.Contains(t, output, "users/, sessions/, tokens/")
		assert.Contains(t, output, "sessions/")
		assert.Contains(t, output, "250")
	})

	t.Run("table format completed with all fields", func(t *testing.T) {
		resp := &CacheWarmResponse{
			Prefixes:    []string{"data/"},
			State:       "completed",
			StartedAt:   now,
			CompletedAt: completedAt,
			KeysWarmed:  5000,
			KeysFailed:  0,
			Duration:    5 * time.Minute,
			Timestamp:   now,
			Success:     true,
			Message:     "All keys warmed successfully",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "completed")
		assert.Contains(t, output, "5000")
		assert.Contains(t, output, "All keys warmed successfully")
	})

	t.Run("default format completed success", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:      "completed",
			KeysWarmed: 1000,
			KeysFailed: 5,
			Duration:   3 * time.Minute,
			Success:    true,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Cache warming completed successfully")
		assert.Contains(t, output, "Keys warmed: 1000")
		assert.Contains(t, output, "Keys failed: 5")
	})

	t.Run("default format completed with errors", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:      "completed",
			KeysWarmed: 800,
			KeysFailed: 200,
			Success:    false,
			Message:    "Some keys could not be warmed",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "completed with errors")
		assert.Contains(t, output, "Some keys could not be warmed")
	})

	t.Run("default format warming in progress", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:         "warming",
			KeysWarmed:    100,
			CurrentPrefix: "users/",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "in progress")
		assert.Contains(t, output, "users/")
		assert.Contains(t, output, "100")
	})

	t.Run("default format warming without current prefix", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:      "warming",
			KeysWarmed: 50,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "in progress")
		assert.Contains(t, output, "50")
	})

	t.Run("default format failed", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:      "failed",
			KeysWarmed: 50,
			KeysFailed: 100,
			Message:    "Connection timeout",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "failed")
		assert.Contains(t, output, "Connection timeout")
		assert.Contains(t, output, "50")
		assert.Contains(t, output, "100")
	})

	t.Run("default format idle with message", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:   "idle",
			Message: "No warming operations",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "No warming operations")
	})

	t.Run("default format unknown state with message", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:   "unknown",
			Message: "Some message",
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatRaw, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Some message")
	})

	t.Run("table format with zero time", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:     "idle",
			Timestamp: time.Time{},
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)
		// Should not panic with zero time
	})

	t.Run("table format no duration", func(t *testing.T) {
		resp := &CacheWarmResponse{
			State:      "warming",
			Timestamp:  now,
			KeysWarmed: 10,
		}
		var buf bytes.Buffer
		err := formatter.FormatCacheWarmResponse(resp, OutputFormatTable, &buf)
		require.NoError(t, err)

		output := buf.String()
		assert.NotContains(t, output, "Duration:")
	})
}

// TestCacheWarmResponseStructure tests the CacheWarmResponse structure.
func TestCacheWarmResponseStructure(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(5 * time.Minute)

	resp := &CacheWarmResponse{
		Prefixes:      []string{"users/", "sessions/"},
		State:         "completed",
		StartedAt:     now,
		CompletedAt:   completedAt,
		KeysWarmed:    1000,
		KeysFailed:    5,
		CurrentPrefix: "sessions/",
		Duration:      5 * time.Minute,
		Timestamp:     now,
		Success:       true,
		Message:       "Cache warming completed successfully",
	}

	assert.Len(t, resp.Prefixes, 2)
	assert.Equal(t, "completed", resp.State)
	assert.Equal(t, int64(1000), resp.KeysWarmed)
	assert.Equal(t, int64(5), resp.KeysFailed)
	assert.Equal(t, "sessions/", resp.CurrentPrefix)
	assert.True(t, resp.Success)

	// Test JSON marshaling
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), "completed")
	assert.Contains(t, string(data), "users/")

	// Test JSON unmarshaling
	var decoded CacheWarmResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, resp.State, decoded.State)
	assert.Equal(t, resp.KeysWarmed, decoded.KeysWarmed)
}

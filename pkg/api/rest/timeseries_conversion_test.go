//go:build rest && timeseries

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestConvertTSQueryResponse(t *testing.T) {
	tests := []struct {
		name  string
		input *service.TSQueryResponse
		check func(*testing.T, *TSQueryResponse)
	}{
		{
			name: "empty response",
			input: &service.TSQueryResponse{
				Metric: "cpu.usage",
				Tags:   map[string]string{"host": "server1"},
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Equal(t, "cpu.usage", resp.Metric)
				assert.Equal(t, map[string]string{"host": "server1"}, resp.Tags)
				assert.Nil(t, resp.Points)
				assert.Nil(t, resp.GroupedResults)
			},
		},
		{
			name: "with points",
			input: &service.TSQueryResponse{
				Metric: "memory.used",
				Tags:   map[string]string{"host": "server2"},
				Points: []*service.TSDataPoint{
					{
						Metric:    "memory.used",
						Tags:      map[string]string{"host": "server2"},
						Value:     1024.5,
						Timestamp: time.Unix(1000, 0),
					},
					{
						Metric:    "memory.used",
						Tags:      map[string]string{"host": "server2"},
						Value:     2048.7,
						Timestamp: time.Unix(2000, 0),
					},
				},
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Equal(t, "memory.used", resp.Metric)
				assert.Len(t, resp.Points, 2)
				assert.Equal(t, 1024.5, resp.Points[0].Value)
				assert.Equal(t, 2048.7, resp.Points[1].Value)
				assert.Equal(t, time.Unix(1000, 0), resp.Points[0].Timestamp)
			},
		},
		{
			name: "with aggregated value",
			input: &service.TSQueryResponse{
				Metric:          "disk.usage",
				Tags:            map[string]string{"host": "server3"},
				AggregatedValue: func() *float64 { v := 75.5; return &v }(),
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Equal(t, "disk.usage", resp.Metric)
				assert.NotNil(t, resp.AggregatedValue)
				assert.Equal(t, 75.5, *resp.AggregatedValue)
			},
		},
		{
			name: "with grouped results without points",
			input: &service.TSQueryResponse{
				Metric: "network.bytes",
				GroupedResults: []*service.TSGroupedResult{
					{
						Tags:            map[string]string{"interface": "eth0"},
						AggregatedValue: func() *float64 { v := 1000000.0; return &v }(),
					},
					{
						Tags:            map[string]string{"interface": "eth1"},
						AggregatedValue: func() *float64 { v := 2000000.0; return &v }(),
					},
				},
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Len(t, resp.GroupedResults, 2)
				assert.Equal(t, map[string]string{"interface": "eth0"}, resp.GroupedResults[0].Tags)
				assert.NotNil(t, resp.GroupedResults[0].AggregatedValue)
				assert.Equal(t, 1000000.0, *resp.GroupedResults[0].AggregatedValue)
				assert.Equal(t, map[string]string{"interface": "eth1"}, resp.GroupedResults[1].Tags)
				assert.NotNil(t, resp.GroupedResults[1].AggregatedValue)
				assert.Equal(t, 2000000.0, *resp.GroupedResults[1].AggregatedValue)
			},
		},
		{
			name: "with grouped results with points",
			input: &service.TSQueryResponse{
				Metric: "cpu.cores",
				GroupedResults: []*service.TSGroupedResult{
					{
						Tags:            map[string]string{"core": "0"},
						AggregatedValue: func() *float64 { v := 50.0; return &v }(),
						Points: []*service.TSDataPoint{
							{
								Metric:    "cpu.cores",
								Tags:      map[string]string{"core": "0"},
								Value:     45.0,
								Timestamp: time.Unix(1000, 0),
							},
							{
								Metric:    "cpu.cores",
								Tags:      map[string]string{"core": "0"},
								Value:     55.0,
								Timestamp: time.Unix(2000, 0),
							},
						},
					},
					{
						Tags:            map[string]string{"core": "1"},
						AggregatedValue: func() *float64 { v := 60.0; return &v }(),
						Points: []*service.TSDataPoint{
							{
								Metric:    "cpu.cores",
								Tags:      map[string]string{"core": "1"},
								Value:     58.0,
								Timestamp: time.Unix(1000, 0),
							},
						},
					},
				},
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Len(t, resp.GroupedResults, 2)

				// First group
				assert.Equal(t, map[string]string{"core": "0"}, resp.GroupedResults[0].Tags)
				assert.NotNil(t, resp.GroupedResults[0].AggregatedValue)
				assert.Equal(t, 50.0, *resp.GroupedResults[0].AggregatedValue)
				assert.Len(t, resp.GroupedResults[0].Points, 2)
				assert.Equal(t, 45.0, resp.GroupedResults[0].Points[0].Value)
				assert.Equal(t, 55.0, resp.GroupedResults[0].Points[1].Value)

				// Second group
				assert.Equal(t, map[string]string{"core": "1"}, resp.GroupedResults[1].Tags)
				assert.NotNil(t, resp.GroupedResults[1].AggregatedValue)
				assert.Equal(t, 60.0, *resp.GroupedResults[1].AggregatedValue)
				assert.Len(t, resp.GroupedResults[1].Points, 1)
				assert.Equal(t, 58.0, resp.GroupedResults[1].Points[0].Value)
			},
		},
		{
			name: "complex response with all fields",
			input: &service.TSQueryResponse{
				Metric:          "http.requests",
				Tags:            map[string]string{"service": "api"},
				AggregatedValue: func() *float64 { v := 1500.0; return &v }(),
				Points: []*service.TSDataPoint{
					{
						Metric:    "http.requests",
						Tags:      map[string]string{"service": "api", "endpoint": "/users"},
						Value:     500.0,
						Timestamp: time.Unix(1000, 0),
					},
				},
				GroupedResults: []*service.TSGroupedResult{
					{
						Tags:            map[string]string{"endpoint": "/users"},
						AggregatedValue: func() *float64 { v := 800.0; return &v }(),
						Points: []*service.TSDataPoint{
							{
								Metric:    "http.requests",
								Tags:      map[string]string{"endpoint": "/users"},
								Value:     800.0,
								Timestamp: time.Unix(1000, 0),
							},
						},
					},
				},
			},
			check: func(t *testing.T, resp *TSQueryResponse) {
				assert.Equal(t, "http.requests", resp.Metric)
				assert.Equal(t, map[string]string{"service": "api"}, resp.Tags)
				assert.NotNil(t, resp.AggregatedValue)
				assert.Equal(t, 1500.0, *resp.AggregatedValue)
				assert.Len(t, resp.Points, 1)
				assert.Len(t, resp.GroupedResults, 1)
				assert.Len(t, resp.GroupedResults[0].Points, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTSQueryResponse(tt.input)
			assert.NotNil(t, result)
			tt.check(t, result)
		})
	}
}

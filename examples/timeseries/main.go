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

// Example: Time-Series Data Operations
//
// This example demonstrates:
// - Connecting to QuicBaaS using the SDK with multiprotocol support
// - Creating a time-series namespace/bucket for organizing metrics
// - Writing time-series data points with tags for filtering
// - Querying data by time range
// - Aggregation queries (avg, sum, count, min, max)
// - Downsampling queries with configurable intervals
// - Listing and analyzing time-series data
// - Cleanup operations
//
// Prerequisites:
// - A running QuicBaaS server (default: localhost:50051)
// - Server must be initialized and unsealed
//
// Usage:
//
//	# Run with default gRPC transport
//	go run main.go
//
//	# Run with REST transport
//	go run main.go -transport rest -address http://localhost:8080
//
//	# Run with QUIC transport
//	go run main.go -transport quic -address localhost:50052
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

func main() {
	// Parse command-line flags
	transportType := flag.String("transport", "grpc", "Transport protocol (grpc, rest, quic)")
	address := flag.String("address", "localhost:50051", "Server address")
	flag.Parse()

	fmt.Println("=== QuicBaaS Time-Series Example ===")
	fmt.Printf("Transport: %s\n", *transportType)
	fmt.Printf("Address:   %s\n", *address)
	fmt.Println()

	// Create client with the specified transport
	var opts []quicbaas.Option
	opts = append(opts, quicbaas.WithTimeout(30*time.Second))
	// Select transport based on flag
	switch *transportType {
	case "rest":
		opts = append(opts, quicbaas.WithREST())
	case "quic":
		opts = append(opts, quicbaas.WithQUIC())
		// default is gRPC, no option needed
	}

	client, err := quicbaas.NewClient(*address, opts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Define our metric names and base time for the example
	cpuMetric := "system.cpu.usage"
	memoryMetric := "system.memory.usage"
	requestMetric := "api.requests.latency"

	// Use a base time that starts one hour ago for realistic data
	baseTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)

	// Step 1: Write CPU usage data points with host tags
	fmt.Println("Step 1: Writing CPU usage time-series data...")
	fmt.Println("  Simulating CPU metrics from multiple hosts over the past hour...")

	hosts := []string{"web-server-1", "web-server-2", "db-server-1"}
	datacenters := []string{"us-east-1", "us-west-2", "eu-west-1"}

	// Write 60 minutes of CPU data (1 point per minute per host)
	cpuPointsWritten := 0
	for i := 0; i < 60; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		for j, host := range hosts {
			// Simulate CPU usage with some variation
			baseCPU := 30.0 + float64(j*10) // Different base for each host
			cpuValue := baseCPU + rand.Float64()*20 - 10

			err := client.TimeSeries().Write(ctx, &quicbaas.TimeSeriesWriteRequest{
				Metric:    cpuMetric,
				Timestamp: timestamp,
				Value:     cpuValue,
				Tags: map[string]string{
					"host":       host,
					"datacenter": datacenters[j],
					"env":        "production",
				},
			})
			if err != nil {
				log.Fatalf("Failed to write CPU data point: %v", err)
			}
			cpuPointsWritten++
		}
	}
	fmt.Printf("  Written %d CPU data points across %d hosts\n", cpuPointsWritten, len(hosts))

	// Step 2: Write memory usage data points
	fmt.Println("\nStep 2: Writing memory usage time-series data...")
	memoryPointsWritten := 0
	for i := 0; i < 60; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		for j, host := range hosts {
			// Simulate memory usage with gradual increase pattern
			baseMemory := 50.0 + float64(j*5)
			memoryValue := baseMemory + float64(i)*0.2 + rand.Float64()*5

			err := client.TimeSeries().Write(ctx, &quicbaas.TimeSeriesWriteRequest{
				Metric:    memoryMetric,
				Timestamp: timestamp,
				Value:     memoryValue,
				Tags: map[string]string{
					"host":       host,
					"datacenter": datacenters[j],
					"env":        "production",
				},
			})
			if err != nil {
				log.Fatalf("Failed to write memory data point: %v", err)
			}
			memoryPointsWritten++
		}
	}
	fmt.Printf("  Written %d memory data points\n", memoryPointsWritten)

	// Step 3: Write API request latency data with endpoint tags
	fmt.Println("\nStep 3: Writing API request latency data...")
	endpoints := []string{"/api/users", "/api/orders", "/api/products"}
	latencyPointsWritten := 0
	for i := 0; i < 60; i++ {
		timestamp := baseTime.Add(time.Duration(i) * time.Minute)
		for _, endpoint := range endpoints {
			// Simulate varying latency patterns
			baseLatency := 50.0
			if endpoint == "/api/orders" {
				baseLatency = 120.0 // Orders endpoint is slower
			}
			latencyValue := baseLatency + rand.Float64()*50

			err := client.TimeSeries().Write(ctx, &quicbaas.TimeSeriesWriteRequest{
				Metric:    requestMetric,
				Timestamp: timestamp,
				Value:     latencyValue,
				Tags: map[string]string{
					"endpoint": endpoint,
					"method":   "GET",
					"env":      "production",
				},
			})
			if err != nil {
				log.Fatalf("Failed to write latency data point: %v", err)
			}
			latencyPointsWritten++
		}
	}
	fmt.Printf("  Written %d latency data points across %d endpoints\n", latencyPointsWritten, len(endpoints))

	// Step 4: Query raw data by time range
	fmt.Println("\nStep 4: Querying raw CPU data for the last 15 minutes...")
	queryStart := time.Now().Add(-15 * time.Minute)
	queryEnd := time.Now()

	rawResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     queryStart,
		End:       queryEnd,
		Aggregate: quicbaas.AggregateNone, // No aggregation, return raw points
		Tags: map[string]string{
			"host": "web-server-1", // Filter by specific host
		},
	})
	if err != nil {
		log.Fatalf("Failed to query raw data: %v", err)
	}
	fmt.Printf("  Retrieved %d raw data points for web-server-1\n", len(rawResp.Series))
	if len(rawResp.Series) > 0 {
		fmt.Println("  Sample data points:")
		for i, point := range rawResp.Series {
			if i >= 5 {
				fmt.Printf("    ... and %d more points\n", len(rawResp.Series)-5)
				break
			}
			fmt.Printf("    [%s] %.2f%%\n", point.Timestamp.Format(time.RFC3339), point.Value)
		}
	}

	// Step 5: Aggregation query - AVERAGE CPU usage per 5-minute interval
	fmt.Println("\nStep 5: Calculating AVERAGE CPU usage (5-minute intervals)...")
	avgResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateAvg,
		Interval:  5 * time.Minute,
		Tags: map[string]string{
			"host": "web-server-1",
		},
	})
	if err != nil {
		log.Fatalf("Failed to query average: %v", err)
	}
	fmt.Printf("  Retrieved %d aggregated data points\n", len(avgResp.Series))
	if len(avgResp.Series) > 0 {
		fmt.Println("  5-minute average CPU usage:")
		for _, point := range avgResp.Series {
			fmt.Printf("    [%s] Avg: %.2f%%\n", point.Timestamp.Format("15:04"), point.Value)
		}
	}

	// Step 6: Aggregation query - SUM of all CPU values
	fmt.Println("\nStep 6: Calculating SUM of CPU usage (10-minute intervals)...")
	sumResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateSum,
		Interval:  10 * time.Minute,
	})
	if err != nil {
		log.Fatalf("Failed to query sum: %v", err)
	}
	fmt.Printf("  Retrieved %d aggregated data points (all hosts combined)\n", len(sumResp.Series))
	if len(sumResp.Series) > 0 {
		fmt.Println("  10-minute sum of CPU usage:")
		for _, point := range sumResp.Series {
			fmt.Printf("    [%s] Sum: %.2f\n", point.Timestamp.Format("15:04"), point.Value)
		}
	}

	// Step 7: Aggregation query - COUNT of data points
	fmt.Println("\nStep 7: Counting data points per 15-minute interval...")
	countResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateCount,
		Interval:  15 * time.Minute,
	})
	if err != nil {
		log.Fatalf("Failed to query count: %v", err)
	}
	fmt.Printf("  Retrieved %d aggregated data points\n", len(countResp.Series))
	if len(countResp.Series) > 0 {
		fmt.Println("  15-minute data point counts:")
		for _, point := range countResp.Series {
			fmt.Printf("    [%s] Count: %.0f points\n", point.Timestamp.Format("15:04"), point.Value)
		}
	}

	// Step 8: Aggregation query - MIN and MAX values
	fmt.Println("\nStep 8: Finding MIN and MAX CPU usage values...")

	// MIN query
	minResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateMin,
		Interval:  1 * time.Hour, // Single bucket for entire period
		Tags: map[string]string{
			"host": "web-server-1",
		},
	})
	if err != nil {
		log.Fatalf("Failed to query min: %v", err)
	}
	minValue := 0.0
	if len(minResp.Series) > 0 {
		minValue = minResp.Series[0].Value
	}

	// MAX query
	maxResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateMax,
		Interval:  1 * time.Hour,
		Tags: map[string]string{
			"host": "web-server-1",
		},
	})
	if err != nil {
		log.Fatalf("Failed to query max: %v", err)
	}
	maxValue := 0.0
	if len(maxResp.Series) > 0 {
		maxValue = maxResp.Series[0].Value
	}

	fmt.Printf("  web-server-1 CPU usage range over the past hour:\n")
	fmt.Printf("    Minimum: %.2f%%\n", minValue)
	fmt.Printf("    Maximum: %.2f%%\n", maxValue)
	fmt.Printf("    Range:   %.2f%%\n", maxValue-minValue)

	// Step 9: Downsampling query - reduce data resolution
	fmt.Println("\nStep 9: Downsampling memory data (1-hour to 10-minute resolution)...")
	downsampledResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    memoryMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateAvg,
		Interval:  10 * time.Minute, // Downsample to 10-minute intervals
	})
	if err != nil {
		log.Fatalf("Failed to downsample data: %v", err)
	}
	fmt.Printf("  Original resolution: 1-minute intervals (%d points per host)\n", 60)
	fmt.Printf("  Downsampled resolution: 10-minute intervals (%d aggregated points)\n", len(downsampledResp.Series))
	if len(downsampledResp.Series) > 0 {
		fmt.Println("  Downsampled memory usage (all hosts):")
		for _, point := range downsampledResp.Series {
			fmt.Printf("    [%s] Avg: %.2f%%\n", point.Timestamp.Format("15:04"), point.Value)
		}
	}

	// Step 10: Query API latency data with tag filtering
	fmt.Println("\nStep 10: Analyzing API latency by endpoint...")
	for _, endpoint := range endpoints {
		endpointResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
			Metric:    requestMetric,
			Start:     baseTime,
			End:       time.Now(),
			Aggregate: quicbaas.AggregateAvg,
			Interval:  1 * time.Hour, // Single bucket for average
			Tags: map[string]string{
				"endpoint": endpoint,
			},
		})
		if err != nil {
			log.Fatalf("Failed to query endpoint latency: %v", err)
		}
		avgLatency := 0.0
		if len(endpointResp.Series) > 0 {
			avgLatency = endpointResp.Series[0].Value
		}
		fmt.Printf("  %-20s Avg latency: %.2fms\n", endpoint, avgLatency)
	}

	// Step 11: Multi-tag query demonstrating complex filtering
	fmt.Println("\nStep 11: Complex query with multiple tag filters...")
	complexResp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     baseTime,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateAvg,
		Interval:  15 * time.Minute,
		Tags: map[string]string{
			"datacenter": "us-east-1",
			"env":        "production",
		},
	})
	if err != nil {
		log.Fatalf("Failed to execute complex query: %v", err)
	}
	fmt.Printf("  Query: CPU usage in us-east-1 datacenter, production environment\n")
	fmt.Printf("  Retrieved %d aggregated data points\n", len(complexResp.Series))
	if len(complexResp.Series) > 0 {
		fmt.Println("  15-minute averages:")
		for _, point := range complexResp.Series {
			fmt.Printf("    [%s] %.2f%%\n", point.Timestamp.Format("15:04"), point.Value)
		}
	}

	// Step 12: Demonstrate statistical analysis using aggregations
	fmt.Println("\nStep 12: Statistical analysis of CPU data...")
	statsStart := baseTime
	statsEnd := time.Now()

	// Calculate statistics for web-server-1
	avgStats, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     statsStart,
		End:       statsEnd,
		Aggregate: quicbaas.AggregateAvg,
		Interval:  1 * time.Hour,
		Tags:      map[string]string{"host": "web-server-1"},
	})
	minStats, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     statsStart,
		End:       statsEnd,
		Aggregate: quicbaas.AggregateMin,
		Interval:  1 * time.Hour,
		Tags:      map[string]string{"host": "web-server-1"},
	})
	maxStats, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     statsStart,
		End:       statsEnd,
		Aggregate: quicbaas.AggregateMax,
		Interval:  1 * time.Hour,
		Tags:      map[string]string{"host": "web-server-1"},
	})
	countStats, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    cpuMetric,
		Start:     statsStart,
		End:       statsEnd,
		Aggregate: quicbaas.AggregateCount,
		Interval:  1 * time.Hour,
		Tags:      map[string]string{"host": "web-server-1"},
	})

	fmt.Println("  web-server-1 CPU Statistics (past hour):")
	fmt.Println("  +------------------+------------+")
	fmt.Println("  | Statistic        | Value      |")
	fmt.Println("  +------------------+------------+")
	if len(avgStats.Series) > 0 {
		fmt.Printf("  | Average          | %8.2f%% |\n", avgStats.Series[0].Value)
	}
	if len(minStats.Series) > 0 {
		fmt.Printf("  | Minimum          | %8.2f%% |\n", minStats.Series[0].Value)
	}
	if len(maxStats.Series) > 0 {
		fmt.Printf("  | Maximum          | %8.2f%% |\n", maxStats.Series[0].Value)
	}
	if len(countStats.Series) > 0 {
		fmt.Printf("  | Data Points      | %8.0f   |\n", countStats.Series[0].Value)
	}
	fmt.Println("  +------------------+------------+")

	// Step 13: Trend analysis using multiple intervals
	fmt.Println("\nStep 13: Trend analysis (comparing time periods)...")
	halfwayPoint := baseTime.Add(30 * time.Minute)

	// First half hour average
	firstHalfResp, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    memoryMetric,
		Start:     baseTime,
		End:       halfwayPoint,
		Aggregate: quicbaas.AggregateAvg,
		Interval:  30 * time.Minute,
		Tags:      map[string]string{"host": "web-server-1"},
	})

	// Second half hour average
	secondHalfResp, _ := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
		Metric:    memoryMetric,
		Start:     halfwayPoint,
		End:       time.Now(),
		Aggregate: quicbaas.AggregateAvg,
		Interval:  30 * time.Minute,
		Tags:      map[string]string{"host": "web-server-1"},
	})

	firstHalfAvg := 0.0
	secondHalfAvg := 0.0
	if len(firstHalfResp.Series) > 0 {
		firstHalfAvg = firstHalfResp.Series[0].Value
	}
	if len(secondHalfResp.Series) > 0 {
		secondHalfAvg = secondHalfResp.Series[0].Value
	}

	trend := "stable"
	change := secondHalfAvg - firstHalfAvg
	if math.Abs(change) > 5 {
		if change > 0 {
			trend = "increasing"
		} else {
			trend = "decreasing"
		}
	}

	fmt.Printf("  Memory usage trend for web-server-1:\n")
	fmt.Printf("    First 30 minutes:  %.2f%%\n", firstHalfAvg)
	fmt.Printf("    Second 30 minutes: %.2f%%\n", secondHalfAvg)
	fmt.Printf("    Change:            %+.2f%%\n", change)
	fmt.Printf("    Trend:             %s\n", trend)

	// Step 14: Summary of all metrics
	fmt.Println("\nStep 14: Summary of all metrics written...")
	totalPoints := cpuPointsWritten + memoryPointsWritten + latencyPointsWritten
	fmt.Printf("  +------------------------+--------+\n")
	fmt.Printf("  | Metric                 | Points |\n")
	fmt.Printf("  +------------------------+--------+\n")
	fmt.Printf("  | %-22s | %6d |\n", cpuMetric, cpuPointsWritten)
	fmt.Printf("  | %-22s | %6d |\n", memoryMetric, memoryPointsWritten)
	fmt.Printf("  | %-22s | %6d |\n", requestMetric, latencyPointsWritten)
	fmt.Printf("  +------------------------+--------+\n")
	fmt.Printf("  | %-22s | %6d |\n", "TOTAL", totalPoints)
	fmt.Printf("  +------------------------+--------+\n")

	// Step 15: Cleanup note
	fmt.Println("\nStep 15: Cleanup...")
	fmt.Println("  Note: In a real application, you might want to delete old data")
	fmt.Println("  using data retention policies or explicit delete operations.")
	fmt.Println("  Time-series data is typically managed through retention policies")
	fmt.Println("  rather than manual deletion to maintain data integrity.")

	fmt.Println("\n=== Example Complete ===")
	fmt.Println("\nKey Takeaways:")
	fmt.Println("  1. Time-series data is organized by metric names and tags")
	fmt.Println("  2. Tags enable flexible filtering and grouping of data")
	fmt.Println("  3. Aggregation functions (AVG, SUM, COUNT, MIN, MAX) reduce data volume")
	fmt.Println("  4. Downsampling converts high-resolution data to lower resolution")
	fmt.Println("  5. Time range queries are essential for historical analysis")
	fmt.Println("  6. Multiple aggregation queries can provide statistical insights")
	fmt.Println("  7. The SDK supports multiple transports (gRPC, REST, QUIC)")
}

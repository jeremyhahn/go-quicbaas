# Data → Time-Series

## Purpose

The Time-Series view ingests, queries, and manages time-series metrics when the time-series subsystem is enabled. Use this view to collect performance metrics, sensor data, or historical trends, then visualize and analyze them with flexible time-range queries and retention policies.

## Layout

The page is structured as a tabbed interface:

**Tabs** — "Ingest", "Query", "Visualize", "Settings"

**Tab: Ingest**

```
Metric Name:    [cpu_usage________]
Timestamp:      [2026-04-25 14:35:00]  (defaults to now)
Tags:           [host: prod-01, region: us-west]
Value:          [75.5]  (numeric value)
Attributes:     [{"unit":"percent"}]  (optional JSON)

                [Clear] [Ingest]
```

- **Metric Name** — e.g., `cpu_usage`, `memory_free`, `request_latency`
- **Timestamp** — when the metric was recorded (defaults to current time)
- **Tags** — key-value metadata for filtering (e.g., host, region, env)
- **Value** — numeric metric value
- **Attributes** — optional JSON for additional metadata

**Tab: Query**

```
Metric Name:    [cpu_usage________]
Time Range:     [2026-04-25 00:00 ▼] to [2026-04-25 23:59 ▼]
                Preset: [Last Hour] [Last 24h] [Last 7d]
Tags Filter:    [host=prod-01, region=us-west]  (optional)
Aggregation:    [Average ▼]  (or Sum, Min, Max, P95, P99)
Interval:       [1m ▼]  (bucket size: 1m, 5m, 1h, 1d)

                [Query]

Results:

Time             Value       Count
───────────────────────────────────
2026-04-25 14:00  72.3       60
2026-04-25 14:01  73.1       60
2026-04-25 14:02  75.5       60
```

- **Metric Name** — which metric to query
- **Time Range** — start and end timestamps (with presets)
- **Tags Filter** — optional constraint (e.g., only prod servers)
- **Aggregation** — how to combine values in each interval (average, sum, max, etc.)
- **Interval** — bucket size (1 minute, 5 minutes, etc.)
- **Results** — table of time-bucketed values

**Tab: Visualize**

```
Metric Name:    [cpu_usage________]
Time Range:     [Last 7 days ▼]
Chart Type:     [Line Chart ▼]  (or Bar, Heatmap, Scatter)

[Line chart showing cpu_usage over 7 days with Y-axis: %, X-axis: Date/Time]

Legend:
  Average (line)
  Max (shaded area)
  Min (shaded area)

[Download as CSV] [Download as PNG]
```

- Renders charts with optional legend
- Export options for CSV and PNG

**Tab: Settings**

```
Time-Series Subsystem Status: ✓ Enabled

Retention Policy:     30 days
Default Interval:     1 minute
Storage Backend:      PebbleDB
Compression:          ✓ Enabled
Query Timeout:        30s

[Save Settings]
```

- Shows subsystem configuration
- Retention policy controls how long metrics are kept

## Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Ingest Metric | Upload | Records a single metric data point | `POST /api/v1/timeseries/ingest` |
| Query Metrics | Search | Retrieves and aggregates metrics over a time range | `POST /api/v1/timeseries/query` |
| Visualize Metrics | Chart | Renders a chart for visual analysis | — (client-side rendering; uses Query endpoint) |
| Download Results | Download | Exports results as CSV or PNG | — (client-side export) |

## Walkthrough

### Ingest and query your first metric

1. Open the Time-Series view (`#/data/timeseries`).
2. Check the **Settings** tab to ensure the Time-Series subsystem is **Enabled**. If disabled:

```
Time-Series subsystem is not enabled.
Enable it in the daemon configuration:
  [Enable] [View Docs]
```

3. Click the **Ingest** tab.
4. In **Metric Name**, enter `cpu_usage`.
5. **Timestamp** defaults to now; leave it.
6. In **Tags**, enter:

```
host: server-01
region: us-west
env: prod
```

7. In **Value**, enter `75.5`.
8. Leave **Attributes** empty (optional).
9. Click **Ingest**.

**Expected outcome:**

```
✓ Tab resets
✓ Success toast: "Metric ingested: cpu_usage"
```

10. Ingest a few more metrics to create a time series:
    - Click **Ingest**, enter cpu_usage with Value 76.2 (timestamp ~1 minute later)
    - Click **Ingest**, enter cpu_usage with Value 74.8 (timestamp ~2 minutes later)

11. Click the **Query** tab.
12. **Metric Name** defaults to "cpu_usage" (if you just ingested it).
13. **Time Range** defaults to "Last 24 hours". Leave it.
14. Leave **Tags Filter** empty (or enter `host=server-01` to filter).
15. **Aggregation** defaults to "Average".
16. **Interval** defaults to "1m" (one-minute buckets).
17. Click **Query**.

**Expected outcome:**

```
✓ Results table appears with bucketed data:
  Time              Value       Count
  ────────────────────────────────────
  2026-04-25 14:35  75.5        1
  2026-04-25 14:36  76.2        1
  2026-04-25 14:37  74.8        1
```

### Visualize metrics

18. Click the **Visualize** tab.
19. **Metric Name** is "cpu_usage" (persists from previous query).
20. **Time Range** defaults to "Last 7 days".
21. **Chart Type** defaults to "Line Chart".
22. Click the chart area (or a refresh button if present) to render the visualization.

**Expected outcome:**

```
✓ A line chart appears showing cpu_usage over the selected time range
✓ Y-axis is labeled with the metric unit (%)
✓ X-axis shows dates/times
✓ Legend shows: Average (line), Max (shaded), Min (shaded)
```

23. Try changing **Chart Type** to "Bar" or "Heatmap" and observe the visualization update.
24. Click **Download as CSV** to export the underlying data.

**Expected outcome:**

```
✓ Browser downloads a file: timeseries-cpu_usage-2026-04-18-to-2026-04-25.csv
✓ CSV contains columns: Time, Value, Count
```

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Time-Series subsystem is not enabled" message | Feature is disabled in daemon config | Enable time-series: `quicbaas config set timeseries.enabled true` |
| Ingest button is disabled | Required fields (Metric Name, Value) are empty | Fill in all required fields |
| Query returns "No data found" | No metrics with that name in the time range | Ingest at least one metric first; check time range |
| Chart does not render | Visualization library failed to load | Refresh the page; check browser console for errors |
| Export as PNG shows blank image | Browser could not capture the chart | Try export as CSV instead; take a screenshot manually |
| Tags filter returns no results | Tag values do not match exactly | Check tag case-sensitivity; verify the tag exists in the data |
| Query is slow for large time ranges | Too many data points to aggregate | Increase the **Interval** (e.g., 1h instead of 1m); reduce the time range |

## See also

- [CLI: quicbaas timeseries](../cli/timeseries.md) — manage time-series via CLI
- [timeseries/README.md](../timeseries/README.md) — time-series architecture and configuration
- [timeseries/retention.md](../timeseries/retention.md) — retention policy details
- [Data → KV Store](data-kv.md) — entity-typed key-value storage

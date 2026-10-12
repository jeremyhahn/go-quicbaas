# Desktop GUI — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented usage, in the order tasks are done. Feature folder: `docs/desktop-gui/`.

This section provides comprehensive, step-by-step walkthroughs for every view in the QuicBaaS web UI and Wails desktop app. Each page covers layout, actions, a complete example workflow, and troubleshooting.

## Software Factory

The Software Factory (the orchestration subsystem) manages stateless inference, sessions, jobs, agents, workflows, and event streams.

| View | Purpose |
|------|---------|
| Dashboard | System health, uptime, service status, and resource counts |
| Software Factory → Jobs | Queue, monitor, and cancel orchestration jobs |
| Software Factory → Fleet | Register, view, and decommission fleet node daemons |
| Software Factory → Sessions | Spawn, prompt, attach, and close persistent sessions |
| Software Factory → Tasks | Create, claim, complete, and decompose work items |
| Software Factory → Workflows | Define, list, edit, duplicate, and run workflows |
| Software Factory → Live Events | Stream job/session events in real time |
| Software Factory → Cost Tracking | Read the consumption ledger: tokens spent per project, task and job, and the account that paid |
| Software Factory → Inference | Submit stateless prompts to fleet-routed backends |

## Data

Storage, retrieval, and management of key-value, vector, time-series, and object data.

| View | Purpose |
|------|---------|
| Data → KV Store | Entity-typed key-value put/get/delete operations |
| Data → Vectors | Vector search and embedding operations |
| Data → Time-Series | Time-series metric ingestion and queries |
| Data → Objects | Blob storage, tagging, and lifecycle operations |

## AI & ML

Retrieval-augmented generation, embeddings, and LLM integrations.

| View | Purpose |
|------|---------|
| AI → RAG | Ingest documents, query, manage namespaces |
| AI → Embeddings | Configure providers, set defaults, manage cache |

## Async

Messaging and scheduled job execution.

| View | Purpose |
|------|---------|
| Async → Messaging | Publish/subscribe channels and dedup policies |
| Async → Scheduler | Cron jobs: create, run, pause, resume, delete |

## Security

Identity, access control, cryptography, and key management.

| View | Purpose |
|------|---------|
| Security → IAM | Users, roles, policies; assign/revoke user roles |
| Security → KMS | Create, rotate, and manage encryption keys |
| Security → Secrets | Store, rotate, and version secrets |
| Security → Seal | Initialize, unseal, rekey, and rotate root key |
| Security → SSH | Generate, import keys; manage known hosts |

## Getting Started

New to QuicBaaS? Start here:

1. Dashboard — check system health
2. Fleet — register or view deployed agents
3. Inference — submit your first prompt
4. KMS — create an encryption key if needed

## Cross-Reference

- See the [CLI Reference](../software-factory/cli.md) for command-line equivalents of each GUI action
- See [agents/inference.md](../software-factory/user.md) for low-level architecture and API details
- See [security/](../security-hardening) for in-depth encryption, IAM, and KMS design docs


---

## Dashboard

### Purpose

The Dashboard is the landing page (route `#/`) and provides a system-wide health overview at a glance. It displays daemon uptime, service status (daemon, orchestrator, storage, KMS, scheduler), and high-level resource counts (sessions, agents, jobs). Use this page to quickly assess the health of the entire QuicBaaS deployment and identify any degraded subsystems.

### Layout

The Dashboard is structured as a full-width card layout with two main sections:

**Top Section — Service Health**

- **Daemon** — liveness check via `GET /api/v1/health/live`. Shows status badge (ok/degraded/down) and uptime in human-readable format (e.g., "2m 52s").
- **Software Factory** — health check via `GET /api/v1/orchestrator/health`. Clickable: navigates to `#/orchestration` on click.
- **Storage (PebbleDB)** — data layer health. Clickable: navigates to KMS view if enabled.
- **KMS** — key management service status. Clickable: navigates to `#/security/kms`.
- **Scheduler** — background job scheduler health. Clickable: navigates to `#/scheduler`.

Each service shows a status badge (green checkmark for "ok", yellow triangle for "degraded", red X for "down").

**Bottom Section — Resource Counts**

Four stat cards displayed side-by-side (or stacked on small screens):

- **Sessions** — count of active sessions; clickable link to `#/orchestration/sessions`
- **Agents** — count of registered fleet node daemons; clickable link to `#/orchestration/agents`
- **Jobs** — count of queued/running orchestration jobs; clickable link to `#/orchestration/jobs`
- **Workflows** — count of defined workflows; clickable link to `#/orchestration/workflows`

Each stat card shows an icon (Session = MessageSquare, Agent = Bot, Job = Briefcase, Workflow = Workflow) and the numeric count.

### Lifecycle actions

The Dashboard is read-only. All actions navigate to the relevant view:

| Action | Navigate to | Endpoint |
|--------|-------------|----------|
| Click Daemon uptime | —  | `GET /api/v1/health/live` |
| Click Software Factory | Software Factory | `GET /api/v1/orchestrator/health` |
| Click Sessions count | Sessions list | `GET /api/v1/orchestrator/sessions` |
| Click Agents count | Fleet | `GET /api/v1/fleet` |
| Click Jobs count | Jobs list | `GET /api/v1/orchestrator/jobs` |
| Click Workflows count | Workflows list | `GET /api/v1/orchestrator/workflows` |

### Walkthrough

#### Create your first session and view it in the dashboard

1. Open the Dashboard (`#/`).
2. Note the current **Sessions** count (e.g., 0).
3. Click the **Sessions** count card to navigate to the Sessions view.
4. Click **+ Spawn Session** and follow the Sessions walkthrough to create a session.
5. After spawning, navigate back to the Dashboard (`#/`).
6. Refresh the page (Ctrl+R or Cmd+R).
7. The **Sessions** count should now show 1.

**Expected outcome:**

```
✓ Dashboard loads with system health displayed
✓ All service badges show "ok" (green)
✓ Sessions count updates after spawning
✓ Uptime increments as time passes
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Service badge shows "degraded" | Daemon or subsystem is under load or recovering | Check logs via CLI: `quicbaas health live` |
| Service badge shows "down" (red X) | Daemon is unreachable or crashed | Restart the daemon: `quicbaas daemon start` |
| Counts show 0 but I created resources | Counts cache is stale | Refresh the page |
| Health check times out | Network connectivity issue | Check network connectivity; verify daemon is running |
| Cannot click service badges | Service link is disabled for down services | Fix the underlying service first |

### See also

- CLI: quicbaas health — command-line health checks
- Software Factory → Sessions — detailed sessions walkthrough
- Software Factory → Fleet — detailed fleet node registration walkthrough


---

## Data → KV Store

### Purpose

The KV Store view provides a graphical interface for entity-typed key-value operations: put (store), get (retrieve), delete, and list operations. Use this view to manage application data, configurations, or test the KV layer without using the CLI or API directly.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Put", "Get", "List", "Delete"

**Tab: Put**

```
Entity Type:  [String ▼]        (or Binary, JSON, etc.)
Key:          [my-config-key________]
Value:        [{"color":"blue"}________]
              (or text area for large values)

              [Clear] [Store]
```

- **Entity Type** — dropdown selecting the value type (String, Binary, JSON, Protobuf, etc.)
- **Key** — string key name
- **Value** — text area or structured editor depending on type
- **Store** button — saves the key-value pair

**Tab: Get**

```
Entity Type:  [String ▼]
Key:          [my-config-key________]

              [Search]

Result:
{
  "key": "my-config-key",
  "value": {"color":"blue"},
  "type": "json",
  "created": "2026-04-25T14:30:00Z",
  "version": 1
}

[Copy] [Delete]
```

- **Search** button — retrieves the key
- **Result** section — displays the value with metadata
- **Copy** button — copies value to clipboard
- **Delete** button — deletes the key

**Tab: List**

```
Entity Type:  [String ▼]
Prefix:       [my-________]      (optional)

              [List]

Results:

my-config-key      String     2026-04-25T14:30:00Z   [View] [Delete]
my-other-key       JSON       2026-04-25T14:20:00Z   [View] [Delete]
my-secret          Binary     2026-04-25T14:10:00Z   [View] [Delete]
```

- **Prefix** — optional filter to list keys matching a prefix
- **Results** — table of keys with type, timestamp, and actions

**Tab: Delete**

```
Entity Type:  [String ▼]
Key:          [my-config-key________]

              [Confirm Delete]

⚠️  This action cannot be undone.
```

- **Confirm Delete** — removes the key permanently

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Store | Save | Stores or updates a key-value pair | `PUT /api/v1/kv/{type}/{key}` |
| Retrieve | Search | Fetches a value by key and type | `GET /api/v1/kv/{type}/{key}` |
| List | List | Lists all keys of a type (optionally filtered by prefix) | `GET /api/v1/kv/{type}` |
| Delete | Trash | Removes a key permanently | `DELETE /api/v1/kv/{type}/{key}` |

### Walkthrough

#### Store and retrieve your first key-value pair

1. Open the KV Store view (`#/data/kv`).
2. Click the **Put** tab (should be default).
3. Set **Entity Type** to "String".
4. In **Key**, enter `greeting`.
5. In **Value**, enter `Hello, QuicBaaS!`.
6. Click **Store**.

**Expected outcome:**

```
✓ Tab resets (fields clear)
✓ Success toast: "Key stored: greeting"
```

7. Click the **Get** tab.
8. **Entity Type** should still be "String" (persists).
9. In **Key**, enter `greeting`.
10. Click **Search**.

**Expected outcome:**

```
✓ Result section appears:
  {
    "key": "greeting",
    "value": "Hello, QuicBaaS!",
    "type": "string",
    "created": "2026-04-25T14:35:00Z",
    "version": 1
  }
✓ [Copy] button is enabled
```

11. Click **Copy** to copy the value to clipboard. You can paste it elsewhere to verify.

#### List and filter keys

12. Click the **List** tab.
13. **Entity Type** is still "String".
14. Leave **Prefix** empty and click **List**.

**Expected outcome:**

```
✓ Results table shows:
  greeting     String     2026-04-25T14:35:00Z   [View] [Delete]
```

15. Store a few more keys so we can test prefix filtering:
    - Click **Put** tab, store `greeting-es: Hola, QuicBaaS!`
    - Click **Put** tab, store `greeting-fr: Bonjour, QuicBaaS!`

16. Click **List** tab again.
17. In **Prefix**, enter `greeting-`.
18. Click **List**.

**Expected outcome:**

```
✓ Results show only keys starting with "greeting-":
  greeting-es       String     2026-04-25T14:36:00Z   [View] [Delete]
  greeting-fr       String     2026-04-25T14:37:00Z   [View] [Delete]
```

#### Delete a key

19. Click the **Delete** tab.
20. **Entity Type** is "String".
21. In **Key**, enter `greeting-es`.
22. Click **Confirm Delete**.

**Expected outcome:**

```
✓ Tab resets
✓ Warning toast: "Key deleted: greeting-es"
✓ The key no longer appears in List results
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Store button is disabled | Key or value field is empty | Fill in all required fields |
| Search returns "Key not found" | Key does not exist or entity type is wrong | Check spelling; verify entity type matches the stored type |
| Delete confirms but key still appears in List | List cache is stale | Refresh the List tab or refresh the page |
| Entity Type dropdown shows only "String" | KV store is configured with limited types | Check daemon config; enable additional entity types |
| Large values are truncated in the Result section | Result section has a max display size | Use the API for large values: `curl https://localhost:18080/api/v1/kv/string/my-key` |

### See also

- CLI: quicbaas kv — manage KV data via CLI
- Data → Vectors — vector search operations
- [API: database.md](../data-services/api.md) — KV API reference


---

## Data → Objects

### Purpose

The Objects view manages blob storage: upload, download, delete, copy, and organize objects with tagging and lifecycle operations. Use this view to store files, documents, or binary data, then manage retention, replication, and archival policies.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Browse", "Upload", "Upload Batch", "Settings"

**Tab: Browse**

```
Search / Filter:      [________]  (by key prefix)
Backend:              [Default ▼]  (storage backend)
Sort By:              [Name ▼]  (or Size, Modified)

Objects:

Key                    Size      Modified            Actions
─────────────────────────────────────────────────────────────
documents/report.pdf   2.5 MB    2026-04-25T14:30Z  [⋮]
documents/summary.txt  48 KB     2026-04-25T14:29Z  [⋮]
logs/app-2026-04.log   1.2 GB    2026-04-25T14:00Z  [⋮]

Total Size: 3.75 GB | Object Count: 3
```

- **Search** — client-side filter by key prefix
- **Backend** — select storage backend if multiple are available
- **Sort** — by name, size, or modification time
- **Objects** — table with three-dot menu per object

**Tab: Upload**

```
Select File:   [Choose File...]  or drag-drop
Object Key:    [documents/report.pdf______________]
                (defaults to filename; customize as needed)
Tags:          [env:prod, team:analytics________]
                (optional; key:value format)
Storage Class: [Standard ▼]  (or Infrequent, Archive)
Lifecycle:     Enable automatic [Days: 365]  archival
               after X days

               [Clear] [Upload]
```

- **Select File** — browse or drag-drop
- **Object Key** — full path; defaults to the filename
- **Tags** — optional metadata for filtering
- **Storage Class** — hot (Standard), warm (Infrequent), or cold (Archive)
- **Lifecycle** — optional auto-archival rule

**Tab: Upload Batch**

```
Select Files:  [Choose Files...] (multiple; shift+click or drag-drop)

Or paste JSON:
[
  {"key":"documents/report.pdf", "tags":{"env":"prod"}},
  {"key":"documents/summary.txt", "tags":{"env":"prod"}}
]

Storage Class: [Standard ▼]
               [Upload Batch]
```

- Bulk upload with optional JSON config

**Tab: Settings**

```
Object Subsystem Status: ✓ Enabled

Available Backends:
  Default (PebbleDB): 500 GB free
  S3 (aws): 1 TB free
  GCS (gcp): Unlimited (quota: 100 GB/month)

Replication:  [Enabled ▼]
  Target:     [S3 ▼]

Compression:  ✓ Enabled

[Save Settings]
```

- Shows available backends and replication settings

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Upload Object | Upload | Stores a file with optional tags and lifecycle policy | `PUT /api/v1/objects/{key}` |
| Download | Download | Downloads an object to the browser or local filesystem | `GET /api/v1/objects/{key}` |
| Delete | Trash | Removes an object | `DELETE /api/v1/objects/{key}` |
| Copy | Copy | Creates a copy of the object at a new key | `POST /api/v1/objects/{key}/copy` |
| Replicate | Sync | Copies the object to a target storage backend | `POST /api/v1/objects/{key}/replicate` |
| Archive | Archive | Moves the object to cold storage (if configured) | `POST /api/v1/objects/{key}/archive` |
| Restore | Restore | Retrieves an archived object back to hot storage | `POST /api/v1/objects/{key}/restore` |
| View Policy | Eye | Shows the object's retention, replication, and archival settings | `GET /api/v1/objects/{key}/policy` |

### Walkthrough

#### Upload and download your first object

1. Open the Objects view (`#/data/objects`).
2. Click the **Upload** tab.
3. Click **Choose File...** and select a text file (e.g., `report.txt`) from your computer.
4. **Object Key** auto-fills with the filename. You can customize it, e.g., `documents/report.txt`.
5. **Tags** is optional. Leave empty for now.
6. **Storage Class** defaults to "Standard".
7. Leave **Lifecycle** unchecked for now.
8. Click **Upload**.

**Expected outcome:**

```
✓ Progress bar appears
✓ After upload completes, success toast: "Object uploaded: documents/report.txt"
✓ Tab resets
```

9. Click the **Browse** tab.
10. Your new object appears in the table:

```
Key                    Size      Modified
────────────────────────────────────────────
documents/report.txt   2.3 KB    2026-04-25T14:35Z  [⋮]
```

11. Click the **[⋮]** (three-dot menu) for your object.
12. Select **Download**.

**Expected outcome:**

```
✓ Browser downloads the file as "report.txt"
```

#### Copy and replicate objects

13. From the three-dot menu, select **Copy**.
14. A dialog appears:

```
Copy Object

New Key:  [documents/report-backup.txt_____]

[Cancel] [Copy]
```

15. Change the key to `documents/report-backup.txt`.
16. Click **Copy**.

**Expected outcome:**

```
✓ Success toast: "Object copied: documents/report-backup.txt"
✓ Two objects now appear in Browse: report.txt and report-backup.txt
```

17. From the three-dot menu, select **Replicate** (if your setup has multiple backends).
18. A dialog appears:

```
Replicate Object

Target Backend:  [S3 ▼]

[Cancel] [Replicate]
```

19. Select a target backend (e.g., "S3") and click **Replicate**.

**Expected outcome:**

```
✓ Success toast: "Object replicated to S3"
✓ Object now exists on both backends
```

#### Manage object policies

20. From the three-dot menu, select **View Policy**.
21. A modal shows the object's settings:

```
Object Policy: documents/report.txt

Retention:
  Retain Until: never (or a specific date)
  Locked: [No ▼]

Replication:
  Enabled: ✓
  Target: S3
  Sync Interval: hourly

Archival:
  Auto-archive: [Days: 365]

[Edit] [Delete]
```

22. Click **Edit** to modify the policy (e.g., set auto-archival to 90 days instead of 365).

#### Delete an object

23. From the three-dot menu of the backup object, select **Delete**.
24. A confirm modal appears:

```
Delete Object?

documents/report-backup.txt will be permanently removed.

[Cancel] [Delete]
```

25. Click **Delete**.

**Expected outcome:**

```
✓ Success toast: "Object deleted: documents/report-backup.txt"
✓ Backup object disappears from Browse table
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Upload button is disabled | No file selected | Click "Choose File..." and select a file |
| Upload fails with "Key already exists" | An object with that key already exists | Choose a different key name; or check the "Overwrite" option if available |
| Download button appears disabled | Object is archived or being restored | Wait for restoration to complete; or use the Restore action |
| Copy or Replicate fails | Target key already exists or backend is unreachable | Choose a new key; check network and backend status |
| Archival does not trigger automatically | Lifecycle policy not set or time has not elapsed | Edit the object policy; set a smaller number of days for testing |
| Restore is slow | Cold storage provider is slow | This is normal for cold storage (can take hours); check provider status |
| Browse tab shows "No objects" | No objects uploaded yet | Click Upload and follow the walkthrough |

### See also

- CLI: quicbaas objects — manage objects via CLI
- [API: database.md](../data-services/api.md) — objects API reference
- [handlers_objectstorage.go](../../pkg/api/rest/handlers_objectstorage.go) — object storage implementation


---

## Data → Time-Series

### Purpose

The Time-Series view ingests, queries, and manages time-series metrics when the time-series subsystem is enabled. Use this view to collect performance metrics, sensor data, or historical trends, then visualize and analyze them with flexible time-range queries and retention policies.

### Layout

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

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Ingest Metric | Upload | Records a single metric data point | `POST /api/v1/timeseries/ingest` |
| Query Metrics | Search | Retrieves and aggregates metrics over a time range | `POST /api/v1/timeseries/query` |
| Visualize Metrics | Chart | Renders a chart for visual analysis | — (client-side rendering; uses Query endpoint) |
| Download Results | Download | Exports results as CSV or PNG | — (client-side export) |

### Walkthrough

#### Ingest and query your first metric

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

#### Visualize metrics

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

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Time-Series subsystem is not enabled" message | Feature is disabled in daemon config | Enable time-series: `quicbaas config set timeseries.enabled true` |
| Ingest button is disabled | Required fields (Metric Name, Value) are empty | Fill in all required fields |
| Query returns "No data found" | No metrics with that name in the time range | Ingest at least one metric first; check time range |
| Chart does not render | Visualization library failed to load | Refresh the page; check browser console for errors |
| Export as PNG shows blank image | Browser could not capture the chart | Try export as CSV instead; take a screenshot manually |
| Tags filter returns no results | Tag values do not match exactly | Check tag case-sensitivity; verify the tag exists in the data |
| Query is slow for large time ranges | Too many data points to aggregate | Increase the **Interval** (e.g., 1h instead of 1m); reduce the time range |

### See also

- [CLI: quicbaas timeseries](../timeseries/cli.md) — manage time-series via CLI
- [timeseries/README.md](../timeseries/user.md) — time-series architecture and configuration
- [timeseries/retention.md](../timeseries/engineering.md) — retention policy details
- Data → KV Store — entity-typed key-value storage


---

## Data → Vectors

### Purpose

The Vectors view manages vector embeddings and similarity search when the vector subsystem is enabled. Use this view to store vectors, search for similar vectors, and manage vector namespaces. Vectors are commonly used for semantic search, recommendations, and machine learning pipelines.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Search", "Store", "Manage", "Settings"

**Tab: Search**

```
Namespace:    [default________]
Query Vector: [0.1, 0.2, 0.3, ...]
              (JSON array or paste from file)

Top-K:        [10]  (number of results)

              [Search]

Results:

Score   Vector ID        Metadata
─────────────────────────────────
0.987   vec_abc123       {"item":"apple"}
0.934   vec_def456       {"item":"apricot"}
0.812   vec_ghi789       {"item":"avocado"}
```

- **Namespace** — filter to a specific vector namespace (e.g., "products", "articles")
- **Query Vector** — paste a vector as a JSON array
- **Top-K** — limit number of results (default 10)
- **Results** — table of matching vectors sorted by similarity score (1.0 = exact match)

**Tab: Store**

```
Namespace:    [default________]
Vector ID:    [vec_my-item________]
Vector:       [0.1, 0.2, 0.3, ...]
Metadata:     [{"item":"apple"}________]
              (optional JSON object)

              [Clear] [Store]
```

- **Vector ID** — unique identifier for the vector
- **Vector** — JSON array of floats
- **Metadata** — optional JSON object for filtering and display

**Tab: Manage**

```
Namespace:    [default________]

              [List Vectors]

Vectors:

Vector ID           Metadata              Stored
─────────────────────────────────────────────────
vec_abc123          {"item":"apple"}      2026-04-25T14:30:00Z  [Delete]
vec_def456          {"item":"apricot"}    2026-04-25T14:35:00Z  [Delete]
vec_ghi789          {"item":"avocado"}    2026-04-25T14:40:00Z  [Delete]

Namespace Stats:
  Total vectors: 3
  Avg vector size: 1536 dimensions
```

- **List Vectors** — loads vectors in the namespace
- **Namespace Stats** — aggregate metadata

**Tab: Settings**

```
Vector Subsystem Status: ✓ Enabled

Embedding Provider: [OpenAI ▼]
Default Model:      [text-embedding-3-small]

Storage Backend:    PebbleDB
Index Type:         HNSW (Hierarchical Navigable Small World)

[Save Settings]
```

- Shows subsystem status and configuration
- Allows changing the default embedding provider

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Search Vectors | Search | Similarity search for k nearest neighbors | `POST /api/v1/vectors/search` |
| Store Vector | Save | Stores a vector with optional metadata | `POST /api/v1/vectors/store` |
| List Vectors | List | Lists all vectors in a namespace | `GET /api/v1/vectors/namespace/{namespace}` |
| Delete Vector | Trash | Removes a vector | `DELETE /api/v1/vectors/{vectorID}` |

### Walkthrough

#### Store and search your first vector

1. Open the Vectors view (`#/data/vectors`).
2. Check the **Settings** tab to ensure the Vector subsystem is **Enabled**. If disabled, the view shows:

```
Vector subsystem is not enabled.
Enable it in the daemon configuration:
  [Enable] [View Docs]
```

3. Click the **Store** tab.
4. **Namespace** defaults to "default".
5. In **Vector ID**, enter `apple`.
6. In **Vector**, paste a sample embedding (e.g., a 1536-dimensional OpenAI embedding):

```json
[0.001234, 0.005678, ..., 0.002345]
```

7. In **Metadata**, optionally enter:

```json
{"item": "apple", "category": "fruit"}
```

8. Click **Store**.

**Expected outcome:**

```
✓ Tab resets
✓ Success toast: "Vector stored: apple"
```

9. Store a few more vectors for comparison:
   - Click **Store** tab, store `apricot` with a slightly different vector
   - Click **Store** tab, store `orange` with a different vector

10. Click the **Search** tab.
11. In **Query Vector**, paste the same vector you stored for `apple`:

```json
[0.001234, 0.005678, ..., 0.002345]
```

12. Leave **Top-K** as 10 (will return the 3 vectors you stored).
13. Click **Search**.

**Expected outcome:**

```
✓ Results table appears:
  Score   Vector ID        Metadata
  ──────────────────────────────────
  1.000   apple            {"item":"apple","category":"fruit"}
  0.892   apricot          {"item":"apricot","category":"fruit"}
  0.734   orange           {"item":"orange","category":"fruit"}
```

The first result has a perfect score (1.0) because it's identical to the query.

#### Manage vectors

14. Click the **Manage** tab.
15. **Namespace** is "default".
16. Click **List Vectors**.

**Expected outcome:**

```
✓ Vectors table shows all 3 vectors
✓ Namespace Stats shows:
  Total vectors: 3
  Avg vector size: 1536 dimensions
```

17. To delete the `orange` vector, click its **[Delete]** button.
18. A confirm modal appears:

```
Delete vector orange?

This action cannot be undone.

[Cancel] [Delete]
```

19. Click **Delete**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Vector deleted: orange"
✓ orange no longer appears in the Manage list
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Vector subsystem is not enabled" message | Feature is disabled in daemon config | Enable vectors: `quicbaas config set vector.enabled true` |
| Store button is disabled | Required fields (Vector ID, Vector) are empty | Fill in all required fields |
| Search returns "No results" | Query vector is too different from stored vectors | Check that you have stored vectors; try a different query |
| Vector dimensions mismatch error | Query vector has different dimensions than stored vectors | Ensure all vectors use the same embedding model (e.g., all 1536-dim) |
| Namespace dropdown is empty | No vectors stored yet | Store at least one vector first |
| Large vector lists are slow to load | Too many vectors in namespace | Limit search to smaller date range; consider archiving old vectors |

### See also

- CLI: quicbaas vectors — manage vectors via CLI
- [vector/README.md](../vector/user.md) — vector architecture and configuration
- AI → Embeddings — embedding provider settings
- Data → KV Store — entity-typed key-value storage


---

## AI → Embeddings

### Purpose

The Embeddings view configures embedding providers (OpenAI, local models, etc.), sets a system default, enables or disables providers, and monitors embedding cache statistics. Use this view to manage which LLM embedders are available for RAG, vector search, and semantic operations.

### Layout

The page displays a list of embedding providers with configuration and status:

**Header** — "Embeddings" title with "Configure embedding providers" subtitle. Controls:

- **Add Provider** button — registers a new embedding provider
- **Default Provider indicator** — shows which provider is current default (marked with a star or badge)

**Providers List** — Table or card grid:

```
Provider                   Status      Model               Enabled  Default   Actions
────────────────────────────────────────────────────────────────────────────────────
OpenAI                     ✓ OK        text-embedding-3-s  ✓        ★         [⋮]
Local (Ollama)             ✓ OK        nomic-embed-text    ✓        —         [⋮]
Cohere                     ⚠ Offline   embed-english-v3    ✗        —         [⋮]

Cache Statistics:
  Size: 1.2 GB / 5 GB (24%)
  Entries: 12,450
  Hit Rate: 87.3%

[Clear Cache]
```

- **Status** — OK (green), Offline (gray), Error (red)
- **Model** — embedding model identifier
- **Enabled** — toggle to enable/disable provider
- **Default** — star icon for the current default
- **Cache stats** — shared across all providers

**Provider Details Modal** (on click):

```
Provider: OpenAI

API Endpoint:   https://api.openai.com/v1/embeddings
API Key:        [••••••••••••••••••] (masked)
Model:          text-embedding-3-small
Dimensions:     1536
Max Input Length: 8000 tokens

Last Test:      2026-04-25T14:30:00Z (success)

[Test Connection] [Set as Default] [Edit] [Delete] [Close]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Add Provider | Plus | Opens a form to register a new embedding provider | `POST /api/v1/embeddings/providers` |
| Toggle Enable | Switch | Enables or disables a provider | `POST /api/v1/embeddings/providers/{name}/status` |
| Set Default | Star | Marks a provider as the system default | `PUT /api/v1/embeddings/providers/{name}/default` |
| Test Connection | Play | Validates provider connectivity and API key | (implicit in UI; no dedicated endpoint) |
| Edit Provider | Edit | Modifies provider configuration (API key, model, etc.) | `PUT /api/v1/embeddings/providers/{name}` |
| Delete Provider | Trash | Removes a provider; shows confirm modal if it is the default | `DELETE /api/v1/embeddings/providers/{name}` |
| Clear Cache | Trash | Flushes the embedding cache to free memory | (client-side; clearing cache may invoke `POST /api/v1/embeddings/cache/clear`) |

### Walkthrough

#### Add a new embedding provider

1. Open the Embeddings view (`#/ai/embeddings`).
2. Click **+ Add Provider** button.
3. A form modal appears:

```
Register Embedding Provider

Name:               [OpenAI_____]
Type:               [OpenAI ▼]  (or Cohere, Ollama, Local, HuggingFace)
API Endpoint:       [https://api.openai.com/v1/embeddings]
API Key / Token:    [sk-proj-••••••••______________]
Model:              [text-embedding-3-small ▼]

Advanced:
  Dimensions:       [1536]  (auto-detected)
  Max Input Tokens: [8000]  (auto-detected)
  Batch Size:       [100]

                    [Test Connection] [Register] [Cancel]
```

4. Fill in the provider details:
   - **Name**: `openai-prod`
   - **Type**: OpenAI
   - **API Endpoint**: (auto-filled for OpenAI)
   - **API Key**: Enter your OpenAI API key
   - **Model**: `text-embedding-3-small`

5. Click **Test Connection** to verify the API key works.

**Expected outcome:**

```
✓ Success message: "Connection test passed. Model supports 1536 dimensions."
✓ Dimensions and Max Input Tokens auto-populate
```

6. Click **Register**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Provider registered: openai-prod"
✓ New provider appears in the list with status ✓ OK
```

#### Set a default provider

7. The new provider appears in the list. To set it as the default, click its row's three-dot menu.
8. Select **Set as Default**.

**Expected outcome:**

```
✓ Success toast: "Default provider changed to openai-prod"
✓ Star icon appears next to the provider name
✓ Other providers' stars disappear
```

9. Verify the change by checking the **Default Provider** indicator at the top of the page.

#### Enable/disable a provider

10. Click a provider's **Enabled** toggle to disable it.

**Expected outcome:**

```
✓ Toggle switches off
✓ Provider status grays out
✓ The provider is no longer available for new embedding operations
✓ Existing cached embeddings remain (toggle re-enables them without recalculation)
```

11. Toggle it back on to re-enable.

#### Manage cache

12. Scroll down to see **Cache Statistics**:

```
Cache Statistics:
  Size: 1.2 GB / 5 GB (24%)
  Entries: 12,450
  Hit Rate: 87.3%
```

13. Click **Clear Cache** to free memory (useful if cache is full or you want to force re-embedding).

**Expected outcome:**

```
✓ Confirm modal appears:
  "Clear the embedding cache? This will free ~1.2 GB. New embeddings will be 
   recalculated on next use."
  [Cancel] [Clear]
  
✓ After clearing:
  Success toast: "Cache cleared"
  Cache size resets to 0 GB / 5 GB
  Hit rate resets
```

#### Edit and delete providers

14. Click a provider's three-dot menu and select **Edit**.
15. A form pre-fills with the provider's current settings.
16. Modify the API key or model (e.g., switch to `text-embedding-3-large`).
17. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Provider updated: openai-prod"
✓ Provider details in the list update
```

18. To delete a provider, click its three-dot menu and select **Delete**.
19. If it is the default, a warning appears:

```
Delete default provider?

openai-prod is currently the default. If deleted, the system will 
select a new default automatically.

[Cancel] [Delete Anyway]
```

20. Click **Delete Anyway**.

**Expected outcome:**

```
✓ Success toast: "Provider deleted: openai-prod"
✓ Provider disappears from the list
✓ Default switches to another provider (or "None" if all are deleted)
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Register button is disabled | API Key or required fields are empty | Fill in all required fields |
| Test Connection fails with "401 Unauthorized" | API key is invalid or expired | Check your API key; regenerate if needed |
| Test Connection fails with "Network error" | Provider endpoint is unreachable | Verify endpoint URL; check network connectivity; verify firewall rules |
| Cannot set a provider as default | Provider is disabled (Enabled toggle is off) | Enable the provider first by toggling it on |
| Cannot delete the only provider | System requires at least one provider | Add a new provider first; then delete the old one |
| Cache clear is slow | Cache is very large (>10 GB) | Wait; this is normal for large caches |
| Embedding operations still use old model after switching providers | Cache has old embeddings cached | Clear the cache after changing the default provider |

### See also

- CLI: quicbaas embeddings — manage providers via CLI
- [embedding/providers.md](../embeddings/engineering.md) — supported embedding providers and configuration
- [embedding/README.md](../embeddings/user.md) — embedding architecture
- AI → RAG — RAG document ingestion and search
- Data → Vectors — vector storage and similarity search


---

## AI → RAG

### Purpose

The RAG (Retrieval-Augmented Generation) view manages document ingestion, semantic search, and context building for LLM-powered question-answering. Use this view to upload documents, organize them into namespaces, search semantically for similar documents, and generate LLM responses grounded in retrieved context.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Ingest", "Search", "Documents", "Namespaces"

**Tab: Ingest**

```
Namespace:    [default________]        (or create new)
Document ID:  [doc_quarterly-report]   (or auto-generate)
Title:        [Q1 2026 Report______________]  (optional)
Content:      [Paste document text here or upload a file...]
              
              [Choose File...] or [Paste Text]

File Format:  [Auto ▼]  (or PDF, TXT, MD, HTML)

              [Clear] [Ingest]
```

- **Namespace** — organizes documents into logical groups (e.g., "product-docs", "company-wiki")
- **Document ID** — unique identifier; auto-generated if empty
- **Title** — optional display name
- **Content** — document text or file upload
- **File Format** — auto-detect or specify

**Tab: Search**

```
Namespace:    [default________]
Query:        [What is our quarterly revenue?_____]

Top-K:        [5]  (number of results)

              [Search]

Results:

Score   Document ID       Title                Source
─────────────────────────────────────────────────────────
0.92    doc_q1-summary    Q1 2026 Summary      Q1 2026 Report
0.87    doc_revenue       Revenue Breakdown    Finance Doc
0.79    doc_analysis      Market Analysis      Strategic Plan

[Build Context] (generates prompt context from selected results)
```

- **Query** — natural language question
- **Top-K** — number of documents to retrieve
- **Results** — ranked by relevance score
- **Build Context** — constructs a prompt with retrieved documents

**Tab: Documents**

```
Namespace:    [default________]

              [List Documents]

Documents:

Document ID              Title                  Ingested           Size
──────────────────────────────────────────────────────────────────────
doc_quarterly-report     Q1 2026 Report        2026-04-25T14:30Z  4.2 KB
doc_revenue              Revenue Breakdown      2026-04-25T14:25Z  2.1 KB
doc_analysis             Market Analysis        2026-04-25T14:20Z  3.8 KB

Total Documents: 3 | Avg Size: 3.4 KB

[Edit Metadata] [Delete]  (per document)
```

- List and manage documents in the namespace
- Edit metadata or delete documents

**Tab: Namespaces**

```
Namespaces:

Name                  Documents  Created            Actions
────────────────────────────────────────────────────────────
default               3          2026-04-01T00:00Z  [Details] [Delete]
product-docs         12          2026-04-10T10:00Z  [Details] [Delete]
company-wiki         45         2026-04-15T09:00Z  [Details] [Delete]

[+ Create Namespace]
```

- View all namespaces and their document counts
- Create or delete namespaces

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Ingest Document | Upload | Stores a document and generates embeddings | `POST /v1/rag/documents` |
| Search Documents | Search | Semantic similarity search for k nearest documents | `POST /v1/rag/query` (search phase) |
| Build Context | Sparkles | Generates a prompt context from search results | `POST /v1/rag/build-context` |
| List Documents | List | Lists all documents in a namespace | `GET /v1/rag/documents?namespace=X` |
| Delete Document | Trash | Removes a document from the namespace | `DELETE /v1/rag/documents/{id}` |
| Reindex Document | Refresh | Re-embeds a document (after content update) | `POST /v1/rag/documents/{id}/reindex` |
| Create Namespace | Plus | Creates a new namespace for organizing documents | — (client-side; first ingest to a new namespace) |

### Walkthrough

#### Ingest and search your first document

1. Open the RAG view (`#/ai/rag`).
2. Click the **Ingest** tab.
3. **Namespace** defaults to "default".
4. **Document ID** auto-generates a unique ID; you can customize it to `doc_welcome`.
5. **Title** — enter "Welcome to QuicBaaS".
6. **Content** — paste or type:

```
QuicBaaS is a high-performance, multi-faceted distributed key-value database 
built on multi-Raft consensus and PebbleDB storage. It is designed for speed, 
strong consistency, horizontal scalability, security, and operational simplicity.

Key features include:
- Multi-protocol support (gRPC, REST, QUIC, MCP)
- Distributed storage with Raft consensus
- Full-text search and vector search
- KMS and HSM integration
- Audit logging and RBAC
```

7. **File Format** defaults to "Auto".
8. Click **Ingest**.

**Expected outcome:**

```
✓ Tab resets
✓ Success toast: "Document ingested: doc_welcome"
```

9. Click the **Documents** tab.
10. Click **List Documents**.

**Expected outcome:**

```
✓ Table shows your new document:
  doc_welcome     Welcome to QuicBaaS     2026-04-25T14:35Z   1.2 KB
```

11. Click the **Search** tab.
12. **Namespace** defaults to "default".
13. In **Query**, enter: `What is QuicBaaS?`.
14. Leave **Top-K** as 5.
15. Click **Search**.

**Expected outcome:**

```
✓ Results table appears:
  Score   Document ID     Title                Source
  ─────────────────────────────────────────────────────
  1.00    doc_welcome     Welcome to QuicBaaS  Ingest
```

The score is 1.00 because the document is a perfect match for the query.

16. Select the result (click the row; a checkbox should appear).
17. Click **Build Context** (or observe a context panel on the right showing the built context).

**Expected outcome:**

```
✓ A context block appears (or a new modal opens):

Context for LLM:

System: Use the following documents to answer the question.

Documents:
- doc_welcome: "QuicBaaS is a high-performance..."

Question: What is QuicBaaS?

[Copy Context] [Use in Prompt]
```

#### Ingest more documents and test multi-document search

18. Click the **Ingest** tab again.
19. Create a new document with ID `doc_architecture`, title "Architecture", and content:

```
QuicBaaS uses a distributed Raft consensus model for replication 
and PebbleDB as the storage engine. The architecture consists of:

- Leader node: coordinates writes
- Follower nodes: replicate state
- Storage layer: PebbleDB with block-based storage
- API layer: multi-protocol (gRPC, REST, QUIC, MCP)
```

20. Click **Ingest**.
21. Ingest one more document with ID `doc_security`, title "Security", and content about KMS, encryption, etc.
22. Click the **Search** tab.
23. In **Query**, enter: `How does QuicBaaS handle replication?`.
24. Click **Search**.

**Expected outcome:**

```
✓ Results now include both relevant documents:
  Score   Document ID     Title              
  ─────────────────────────────────────────
  0.94    doc_architecture Architecture  
  0.78    doc_welcome     Welcome to QuicBaaS
```

#### Manage namespaces

25. Click the **Namespaces** tab.
26. You should see "default" with 3 documents.
27. Click **[+ Create Namespace]** (or a similar button).
28. A form appears:

```
Create Namespace

Name:  [product-docs___________]

[Cancel] [Create]
```

29. Enter `product-docs` and click **Create**.

**Expected outcome:**

```
✓ "product-docs" now appears in the Namespaces list
```

30. Go back to **Ingest** and ingest a document to the "product-docs" namespace by selecting it in the **Namespace** dropdown.

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Ingest button is disabled | Content is empty | Paste or type document content |
| Search returns "No results" | No documents in namespace | Ingest at least one document first |
| Search returns low-relevance results (score < 0.5) | Query does not match document content | Try rephrasing the query; ingest more relevant documents |
| Embedding generation fails | Embedding provider not configured | Check Settings; configure an embedding provider (OpenAI, local, etc.) |
| Namespaces dropdown is empty | No namespaces created | Ingest a document to "default" namespace first |
| Reindex is slow | Large document being re-embedded | This is normal; wait or check daemon logs |
| Build Context is empty | No documents selected from search results | Click on a search result row to select it before clicking Build Context |

### See also

- [CLI: quicbaas rag](../rag/cli.md) — manage RAG via CLI
- [handlers_rag.go](../../pkg/api/rest/handlers_rag.go) — RAG implementation
- AI → Embeddings — embedding provider configuration
- Data → Vectors — vector storage and search


---

## Async → Messaging

### Purpose

The Messaging view manages publish-subscribe channels and deduplication policies for async event distribution. Use this view to create channels, publish messages, subscribe to topics, and configure message dedup and retention settings.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Channels", "Publish", "Subscribe", "Dedup Policies"

**Tab: Channels**

```
Create Channel:     [+ New Channel]

Channels:

Name                Status      Subscribers   Messages/24h   Actions
────────────────────────────────────────────────────────────────────
user-events         ✓ Active    3             1,245          [⋮]
order-notifications ✓ Active    2             456            [⋮]
analytics-sink      ⚠ Inactive  0             0              [⋮]

[Edit] [Delete] [View Stats]  (per channel)
```

- **Status** — Active (green), Inactive (gray)
- **Subscribers** — count of active subscribers
- **Messages/24h** — throughput indicator
- Three-dot menu for management actions

**Tab: Publish**

```
Channel:     [user-events________]
Message:     [{"event":"login","user":"alice"}___________]
             (JSON or text)
             
Tags:        [source:api, env:prod________]
             (optional; for dedup)

             [Clear] [Publish]

Publish Status:
  Message ID: msg_abc123def456
  Status: ✓ Published
  Subscribers notified: 3
```

- **Channel** — dropdown of available channels
- **Message** — JSON or text content
- **Tags** — optional dedup keys
- **Status** — shows result of publishing

**Tab: Subscribe**

```
Channel:        [user-events________]
Subscriber ID:  [sub_my-app________]  (auto-generated or custom)

                [Subscribe]

Active Subscriptions:

Subscription ID        Channel            Connected   Messages Received
──────────────────────────────────────────────────────────────────────
sub_my-app             user-events        ✓ Yes       156
sub_analytics          user-events        ✓ Yes       156
sub_old-service        order-notifications ✓ No        (offline)

[View Messages] [Unsubscribe]  (per subscription)
```

- Create new subscriptions
- List and manage active subscriptions

**Tab: Dedup Policies**

```
Channel:       [user-events________]
Dedup Mode:    [Content Hash ▼]  (or Tag-based, Time Window, None)

Content Hash:
  Algorithm:   [SHA256 ▼]
  Window:      [5 min ▼]  (time window for dedup)

Tag-based:
  Dedup Tags:  [source, user_id________]
  Window:      [1 hour ▼]

              [Save Policy]

Policy Statistics:
  Duplicates Filtered (24h): 234
  Dedup Efficiency: 15.8%
```

- Configure deduplication by content, tags, or time
- View dedup statistics

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Channel | Plus | Registers a new pub-sub channel | `POST /api/v1/messaging/channels` |
| Delete Channel | Trash | Removes a channel (show warning if subscribers exist) | `DELETE /api/v1/messaging/channels/{name}` |
| Publish Message | Send | Publishes a message to a channel | `POST /api/v1/messaging/publish/{channel}` |
| Subscribe | Check | Creates a subscription to a channel | `POST /api/v1/messaging/subscribe` |
| Unsubscribe | X | Removes a subscription | `DELETE /api/v1/messaging/subscribe/{id}` |
| Update Dedup Policy | Save | Configures deduplication rules | (dedup config is channel-level) |
| View Channel Stats | Eye | Shows throughput, subscriber count, dedup stats | `GET /api/v1/messaging/stats/{channel}` |

### Walkthrough

#### Create a channel and publish messages

1. Open the Messaging view (`#/async/messaging`).
2. Click the **Channels** tab.
3. Click **+ New Channel**.
4. A form appears:

```
Create Channel

Name:        [order-events__________]
Description: [Events from the order service]  (optional)
Retention:   [24 hours ▼]  (or 7d, 30d, infinite)

[Cancel] [Create]
```

5. Enter a channel name: `order-events`.
6. Optionally add a description: "Events from the order service".
7. Set retention to "24 hours".
8. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Channel created: order-events"
✓ Channel appears in the Channels list with status ✓ Active
```

9. Click the **Publish** tab.
10. **Channel** dropdown shows "order-events". Select it (or it defaults if it's the only one).
11. In **Message**, enter a sample message:

```json
{
  "event": "order_placed",
  "order_id": "ORD-12345",
  "user": "alice",
  "amount": 99.99,
  "timestamp": "2026-04-25T14:35:00Z"
}
```

12. In **Tags**, optionally enter: `source:order-service, env:prod`.
13. Click **Publish**.

**Expected outcome:**

```
✓ Publish Status shows:
  Message ID: msg_abc123def456
  Status: ✓ Published
  Subscribers notified: 0 (no subscribers yet)
```

14. Publish a few more messages:

```json
{"event": "order_shipped", "order_id": "ORD-12345", "status": "in_transit"}
{"event": "order_delivered", "order_id": "ORD-12345", "status": "delivered"}
```

#### Subscribe to a channel

15. Click the **Subscribe** tab.
16. **Channel** should show "order-events" (or select it).
17. **Subscriber ID** auto-generates something like `sub_abc123`. You can customize it to `sub_my-app`.
18. Click **Subscribe**.

**Expected outcome:**

```
✓ Modal closes (or tab resets)
✓ Success toast: "Subscription created: sub_my-app"
✓ Active Subscriptions table shows:
  sub_my-app    order-events    ✓ Yes    3
  (Messages Received = 3, from the previous publishes)
```

19. Click **[View Messages]** on your subscription.
20. A panel or modal shows the messages received:

```
Messages Received (3):

Message 1:
{
  "event": "order_placed",
  "order_id": "ORD-12345",
  ...
}

Message 2:
{
  "event": "order_shipped",
  ...
}

Message 3:
{
  "event": "order_delivered",
  ...
}
```

#### Configure deduplication

21. Click the **Dedup Policies** tab.
22. **Channel** defaults to "order-events".
23. **Dedup Mode** defaults to "Content Hash".
24. Leave **Algorithm** as "SHA256" and **Window** as "5 min".
25. Click **Save Policy**.

**Expected outcome:**

```
✓ Success toast: "Dedup policy updated"
✓ Policy Statistics show (initially):
  Duplicates Filtered (24h): 0
  Dedup Efficiency: 0%
```

26. Publish the same message twice:

```json
{"event": "test_duplicate", "id": "1"}
```

27. The second publish will be deduplicated (filtered by content hash).

**Expected outcome:**

```
✓ Second publish shows:
  Status: ⚠ Deduplicated (not published; matched hash)
✓ Subscribers do not receive a duplicate
✓ Dedup statistics update:
  Duplicates Filtered (24h): 1
  Dedup Efficiency: 0.5%
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Channel form shows validation error | Channel name is empty or already exists | Enter a unique channel name |
| Publish button is disabled | Message field is empty | Enter message content (JSON or text) |
| Publish shows "Channel not found" | Channel was deleted or does not exist | Create the channel first via Channels tab |
| Dedup policy does not filter duplicates | Dedup mode is "None" or window has expired | Set Dedup Mode to "Content Hash" or "Tag-based"; adjust window if needed |
| Subscription shows 0 subscribers after publishing | Subscriber list is cached or slow to update | Refresh the page; wait a few seconds |
| Unsubscribe button is disabled | Subscription is already closed or being used | Wait a moment and try again |
| Channel delete fails with "Subscribers exist" | Channel has active subscribers; subscribers must unsubscribe first | Unsubscribe all subscribers before deleting the channel |

### See also

- CLI: quicbaas messaging — manage messaging via CLI
- [handlers_messaging.go](../../pkg/api/rest/handlers_messaging.go) — messaging implementation
- Async → Scheduler — scheduled job execution


---

## Async → Scheduler

### Purpose

The Scheduler view manages cron-like jobs that run on a configurable schedule. Use this view to create recurring jobs, trigger them immediately for testing, pause and resume scheduling, edit job definitions, and delete jobs. The scheduler persists job state and executes them reliably across daemon restarts.

### Layout

The page displays a list of scheduled jobs with filter and search:

**Header** — "Scheduler" title with "Cron jobs and recurring tasks" subtitle. Primary CTA is **+ New Job** button.

**Filters and Search** — Above the table:

- **Status filter** — "All", "Enabled", "Disabled", "Error". Defaults to "All".
- **Search input** — client-side filter on job ID and description.

**Jobs Table** — Columns:

- **Job ID** — unique identifier (e.g., `job_daily-backup`)
- **Description** — human-readable name
- **Schedule** — cron expression (e.g., `0 2 * * *` for 2 AM daily)
- **Status** — badge: "enabled" (green), "disabled" (gray), "error" (red)
- **Last Run** — ISO 8601 timestamp or "Never"
- **Next Run** — ISO 8601 timestamp
- **Actions** — three-dot menu with: Run Now, Pause/Resume, Edit, Delete

**Empty State**:

```
No scheduled jobs.
Create your first job to get started.

[+ New Job button]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Job | Plus | Opens a form to define a new scheduled job | `POST /api/v1/scheduler/jobs` |
| Run Now | Play | Triggers the job immediately (bypasses schedule) | `POST /api/v1/scheduler/jobs/{id}/trigger` |
| Pause Job | Pause | Disables the job; no future runs until resumed | `POST /api/v1/scheduler/jobs/{id}/pause` |
| Resume Job | Play | Re-enables a paused job | `POST /api/v1/scheduler/jobs/{id}/resume` |
| Edit Job | Edit | Opens the job definition for inline modification | `PUT /api/v1/scheduler/jobs/{id}` |
| Delete Job | Trash | Removes a job permanently; shows a confirm modal | `DELETE /api/v1/scheduler/jobs/{id}` |
| View History | History | Shows past execution logs (if not yet implemented, note as missing) | `GET /api/v1/scheduler/jobs/{id}/history` |

### Walkthrough

#### Create your first scheduled job

1. Open the Scheduler view (`#/async/scheduler`).
2. Click **+ New Job** button.
3. A form modal appears:

```
Create Scheduled Job

Job ID:         [daily-backup_______________]  (auto-gen: job_<name>)
Description:    [Daily database backup_____]
Schedule:       [0 2 * * * ▼]  (cron expression; 2 AM daily)
                Preset: [Every hour] [Daily] [Weekly] [Monthly] [Custom...]

Type:           [Command ▼]  (or Webhook, Script, Orchestration Job)

Command Configuration:
  Command:      [/usr/local/bin/backup.sh]
  Args:         [--db prod --retain 30d]
  Timeout:      [1 hour ▼]

Advanced:
  Retry on Failure:    [Enabled ▼]  Max Retries: [3]
  On Success:          [Notify via Email]
  On Failure:          [Notify via Email + Slack]

                [Cancel] [Create]
```

- **Job ID** — unique identifier; auto-generated from name
- **Description** — human-readable purpose
- **Schedule** — cron expression (or use presets)
- **Type** — Command, Webhook, Script, or Orchestration Job
- **Configuration** — depends on type
- **Advanced** — retry policy and notifications

4. Fill in the form:
   - **Job ID**: `daily-backup`
   - **Description**: `Daily database backup`
   - **Schedule**: Keep the preset "Daily" (0 2 * * * = 2 AM daily)
   - **Type**: `Command`
   - **Command**: `/usr/local/bin/backup.sh` (or use a mock command for testing)
   - **Args**: `--db prod --retain 30d`
   - **Timeout**: `1 hour`
   - **Retry**: Enabled with 3 max retries

5. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job created: daily-backup"
✓ New job appears in the table with status "enabled" (green)
✓ "Next Run" shows a future timestamp (2 AM tomorrow if it's after 2 AM today)
```

#### Run a job immediately

6. Find your new job in the table.
7. Click the three-dot menu and select **Run Now**.
8. A confirm modal appears:

```
Run Now?

This will execute the job immediately, bypassing the schedule.

[Cancel] [Run Now]
```

9. Click **Run Now**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job triggered: daily-backup"
✓ "Last Run" timestamp updates to "now"
✓ Job status may briefly show "running" (or change indicator)
✓ After execution, status returns to "enabled"
```

#### Pause and resume a job

10. Click the three-dot menu and select **Pause Job**.
11. A confirm modal appears:

```
Pause Job?

daily-backup will not run on its schedule until you resume it.

[Cancel] [Pause]
```

12. Click **Pause**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job paused: daily-backup"
✓ Job status badge changes to "disabled" (gray)
✓ "Next Run" becomes "—" (no scheduled run)
```

13. Click the three-dot menu and select **Resume Job**.

**Expected outcome:**

```
✓ Success toast: "Job resumed: daily-backup"
✓ Status badge returns to "enabled" (green)
✓ "Next Run" updates to the next scheduled occurrence
```

#### Edit a job

14. Click the three-dot menu and select **Edit Job**.
15. A modal opens with the current job definition:

```
Edit Scheduled Job: daily-backup

Description: [Daily database backup__________]
Schedule:    [0 2 * * * ▼]  (cron expression)
Type:        [Command ▼]
Command:     [/usr/local/bin/backup.sh]
Args:        [--db prod --retain 30d________]
Timeout:     [1 hour ▼]

             [Cancel] [Save]
```

16. Modify the **Schedule** to run more frequently (e.g., `0 * * * *` for every hour instead of daily).
17. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job updated: daily-backup"
✓ Table updates: Schedule now shows "0 * * * *"
✓ "Next Run" recalculates based on the new schedule
```

#### Delete a job

18. Click the three-dot menu and select **Delete Job**.
19. A confirm modal appears:

```
Delete Job?

daily-backup will be permanently removed. This cannot be undone.

[Cancel] [Delete]
```

20. Click **Delete**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Job deleted: daily-backup"
✓ Job disappears from the table
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Job form shows cron validation error | Cron expression is invalid | Use a preset (Daily, Weekly, etc.) or check cron syntax (e.g., `0 2 * * *`) |
| Run Now button is disabled | Job is already running or paused | Resume the job first if paused; wait for current execution to complete |
| "Last Run" shows an error status | Job execution failed | Check daemon logs; verify the command exists and is executable |
| "Next Run" is not updating | Scheduler is paused or daemon is not running | Check daemon status: `quicbaas health live` |
| Pause button is disabled | Job is already paused or being executed | Wait for execution to complete before pausing |
| Edit does not save | Required field is missing or cron is invalid | Fill in all fields; verify cron syntax |
| Delete fails with "Job is running" | Job is currently executing | Wait for execution to complete or click "Run Now" to interrupt (if supported) |

### See also

- CLI: quicbaas scheduler — manage scheduler jobs via CLI
- [handlers_scheduler.go](../../pkg/api/rest/handlers_scheduler.go) — scheduler implementation
- scheduler/README.md — scheduler architecture and configuration
- [scheduler/api-reference-rest.md](../scheduler/api.md) — scheduler REST API
- Async → Messaging — pub-sub messaging


---

## Security → IAM

### Purpose

The IAM (Identity and Access Management) view manages users, roles, and policies across tenants. Use this view to create users, assign roles to users, manage role definitions and permissions, and configure multi-tenant access control. This is the central hub for authentication and authorization in QuicBaaS.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Users", "Roles", "Policies", "Tenants"

**Tab: Users**

```
Tenant:      [default ▼]  (if multi-tenant enabled)
Search:      [________]

Create User: [+ New User]

Users:

User ID              Email                  Roles                 Status    Actions
─────────────────────────────────────────────────────────────────────────────────
user_alice           alice@example.com      admin, data-manager   ✓ Active  [⋮]
user_bob             bob@example.com        viewer                ✓ Active  [⋮]
user_carol           carol@example.com      —                     ⚠ Pending [⋮]

[Edit] [Assign Roles] [Reset Password] [Delete]  (per user)
```

- **User ID** — unique identifier
- **Email** — user email address
- **Roles** — assigned role list
- **Status** — Active (green), Pending (yellow), Disabled (gray)
- Three-dot menu for actions

**Tab: Roles**

```
Search: [________]

Create Role: [+ New Role]

Roles:

Role Name            Description                  Users   Permissions      Actions
────────────────────────────────────────────────────────────────────────────────────
admin                Full system access           2       All              [⋮]
data-manager         Manage data resources        5       KMS, Secrets, KV [⋮]
viewer               Read-only access            12       View all         [⋮]

[Edit] [Duplicate] [Delete]  (per role)
```

- **Role Name** — e.g., `admin`, `data-manager`, `viewer`
- **Description** — what the role is for
- **Users** — count of users with this role
- **Permissions** — list of granted scopes

**Tab: Policies**

```
Policy Type: [RBAC ▼]  (or ABAC)
Tenant:      [default ▼]

Policies:

Policy Name          Type   Target          Condition     Actions
──────────────────────────────────────────────────────────────────
secret-access        RBAC   Secrets API     role=dev      [⋮]
kms-encrypt-only     RBAC   KMS Keys        action=encrypt [⋮]
time-based-access    ABAC   All APIs       hour < 18:00  [⋮]

[+ New Policy] [Edit] [Delete]  (per policy)
```

- **Policy Name** — identifier
- **Type** — RBAC (role-based) or ABAC (attribute-based)
- **Target** — which resource or API the policy applies to
- **Condition** — constraint (role, attribute, time, etc.)

**Tab: Tenants** (if multi-tenant enabled)

```
Tenants:

Tenant ID     Name        Users   Roles    Created            Actions
───────────────────────────────────────────────────────────────────────
default       Default     15      3        2026-01-01T00:00Z  [⋮]
acme-corp     ACME Corp   42      8        2026-02-15T10:00Z  [⋮]
startup-xyz   Startup XYZ  5      2        2026-04-01T14:00Z  [⋮]

[+ New Tenant] [Edit] [Delete]  (per tenant)
```

- **Tenant ID** — unique identifier
- **Name** — display name
- **Users** — count of users in tenant
- **Roles** — count of roles available in tenant
- **Created** — ISO 8601 timestamp

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create User | Plus | Opens a form to register a new user (pending status) | `POST /api/v1/tenants/{tenant}/users` |
| Enroll Authenticator | Key | Generates invite link for user to enroll passkey, security key, or smartcard | `POST /api/v1/tenants/{tenant}/users/{userID}/enroll` |
| Assign Roles | Lock | Opens a role selector; updates user's roles | `POST /api/v1/tenants/{tenant}/users/{userID}/roles` |
| Reset Authenticator | Refresh | Generates a new invite link if user lost their device | `POST /api/v1/tenants/{tenant}/users/{userID}/reset-authenticator` |
| Delete User | Trash | Removes a user from the tenant; shows confirm modal | `DELETE /api/v1/tenants/{tenant}/users/{userID}` |
| Create Role | Plus | Opens a form to define a new role with permissions | `POST /api/v1/tenants/{tenant}/roles` |
| Assign Permissions to Role | Edit | Opens a checklist of permissions; assigns to the role | `PUT /api/v1/tenants/{tenant}/roles/{roleID}` |
| Duplicate Role | Copy | Creates a copy of a role with a new name | `POST /api/v1/tenants/{tenant}/roles` (with copied permissions) |
| Delete Role | Trash | Removes a role; shows warning if users are assigned | `DELETE /api/v1/tenants/{tenant}/roles/{roleID}` |
| Create Policy | Plus | Opens a form for RBAC or ABAC policy creation | `POST /api/v1/tenants/{tenant}/policies` |
| Edit Policy | Edit | Modifies policy rules and conditions | `PUT /api/v1/tenants/{tenant}/policies/{policyID}` |
| Delete Policy | Trash | Removes a policy | `DELETE /api/v1/tenants/{tenant}/policies/{policyID}` |

### Walkthrough

#### Create a user and activate authenticator

1. Open the IAM view (`#/security/iam`).
2. Click the **Users** tab.
3. Click **+ New User** button.
4. A form modal appears:

```
Create User

Tenant:      [default ▼]  (read-only if single-tenant)
Email:       [alice@example.com______________]
Name:        [Alice Smith___________]

Authenticator: [Auto-generate invite link]  (checkbox)

               [Cancel] [Create]
```

5. Enter **Email**: `alice@example.com`.
6. Enter **Name**: `Alice Smith`.
7. Check **Auto-generate invite link** to create an enrollment invitation.
8. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "User created: alice@example.com (pending activation)"
✓ If invite was generated: "Invite link: https://quicbaas.example.com/auth/invite?token=..."
✓ User appears in the Users list with status "Pending" (yellow)
✓ Admin should send the invite URL to alice@example.com
```

9. Alice receives the invite, visits the URL, and enrolls her first passkey or security key.
10. Once she activates, her status changes to "Active" (green).
11. Find the activated user in the Users table (alice@example.com).
12. Click the three-dot menu and select **Assign Roles**.
12. A multi-select modal appears:

```
Assign Roles to alice@example.com

Available Roles:

[ ] admin           (Full system access)
[✓] data-manager    (Manage data resources)
[ ] viewer          (Read-only access)

             [Cancel] [Save]
```

13. Check **admin** and **data-manager** to assign both roles to Alice.
14. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Roles assigned to alice"
✓ Users table updates: alice now shows "admin, data-manager" in the Roles column
```

#### Create a custom role

15. Click the **Roles** tab.
16. Click **+ New Role** button.
17. A form modal appears:

```
Create Role

Role Name:    [kms-operators_______________]
Description:  [Users who manage encryption keys]
Tenant:       [default ▼]

Permissions:

[ ] agents:register       (Register agents)
[✓] kms:generatekey       (Generate KMS keys)
[✓] kms:rotatekey         (Rotate keys)
[✓] kms:deletekey         (Delete keys)
[ ] kms:decrypt           (Decrypt data)
[ ] secrets:read          (Read secrets)
[ ] secrets:write         (Write secrets)

             [Cancel] [Create]
```

18. Enter **Role Name**: `kms-operators`.
19. Enter **Description**: `Users who manage encryption keys`.
20. Check the KMS-related permissions:
    - `kms:generatekey`
    - `kms:rotatekey`
    - `kms:deletekey`
21. Leave `kms:decrypt` unchecked (separation of duties — operators create/rotate but don't decrypt).
22. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Role created: kms-operators"
✓ Role appears in the Roles list with 3 permissions
```

#### Assign a role to a user

23. Go back to the **Users** tab.
24. Create another user (e.g., `kms-admin@example.com`).
25. After creation, click the three-dot menu and select **Assign Roles**.
26. Check **kms-operators** (your newly created role).
27. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Roles assigned"
✓ User now has the `kms-operators` role with its 3 permissions
```

#### Create a policy (RBAC example)

28. Click the **Policies** tab.
29. Click **+ New Policy** button.
30. A form appears:

```
Create Policy

Policy Name:  [secret-restricted__________]
Type:         [RBAC ▼]  (or ABAC)
Tenant:       [default ▼]

RBAC Configuration:

Target:       [Secrets API ▼]  (or KMS, Data, etc.)
Role:         [data-manager ▼]
Action:       [✓ Read] [✓ Write] [✗ Delete]

Effect:       [Allow ▼]  (or Deny)

              [Cancel] [Create]
```

31. Enter **Policy Name**: `secret-restricted`.
32. **Type** is RBAC.
33. **Target**: `Secrets API`.
34. **Role**: `data-manager`.
35. **Action**: Check Read and Write, leave Delete unchecked.
36. **Effect**: Allow.
37. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Policy created: secret-restricted"
✓ Policy appears in the Policies list
```

38. Any user with the `data-manager` role can now read and write secrets, but not delete them.

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create User form shows validation error | Email is invalid or already exists | Use a valid email address; check if the user already exists |
| Assign Roles button is disabled | User is in a different tenant | Switch tenants in the Tenant dropdown; then find the user |
| Role has no permissions assigned | New roles start with no permissions | Click Assign Roles or edit the role to add permissions |
| Delete Role fails with "Users assigned" | Role is still in use by users | Reassign those users to a different role first |
| Reset Password does not send email | Email service is not configured | Check daemon config; set up email integration |
| Policy does not take effect | Policy syntax is invalid or target does not exist | Verify policy type (RBAC vs ABAC); check target resource name |
| Users cannot log in after role change | Role change revokes their permissions | Reassign appropriate role; or ask them to log out and back in |

### See also

- CLI: quicbaas iam — manage IAM via CLI
- iam/README.md — IAM architecture and configuration
- iam/rbac.md — RBAC reference
- [iam/abac.md](../authorization/architecture.md) — ABAC policy syntax
- security/authentication.md — authentication mechanisms
- [security/authorization.md](../authorization/api.md) — authorization policies


---

## Security → KMS

### Purpose

The KMS (Key Management Service) view manages encryption keys: create keys, rotate them, view versions, set key policies, mark defaults, and manage key safety limits. Use this view to govern the cryptographic keys used throughout QuicBaaS for at-rest encryption, AEAD operations, and cryptographic signing.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Keys", "Key Versions", "Safety Limits", "Policies"

**Tab: Keys**

```
Search: [________]  (by key name)

Create Key: [+ Generate Key]

Keys:

Key Name              Type       Status    Default  Created            Actions
──────────────────────────────────────────────────────────────────────────────
master-key-2026      RSA-4096   enabled   ★        2026-01-01T00:00Z  [⋮]
data-encryption      AES-256    enabled   —        2026-02-15T10:00Z  [⋮]
old-key-2025         RSA-2048   disabled  —        2025-12-01T08:00Z  [⋮]

[Generate] [Rotate] [Set Default] [Disable] [Delete]  (per key)
```

- **Key Name** — unique identifier
- **Type** — RSA-2048, RSA-4096, AES-256, etc.
- **Status** — enabled (green), disabled (gray), compromised (red)
- **Default** — star icon if this is the system default key
- **Created** — ISO 8601 timestamp
- Three-dot menu for actions

**Tab: Key Versions**

```
Key Name: [master-key-2026 ▼]  (dropdown to select key)

Versions:

Version   Status     Created            Rotated By   Actions
──────────────────────────────────────────────────────────
v5        active     2026-04-20T12:00Z  system       [Export] [Use] [Delete]
v4        retired    2026-04-10T12:00Z  admin        [Export] [Delete]
v3        retired    2026-03-31T12:00Z  admin        [Export] [Delete]
v2        archived   2026-03-01T12:00Z  system       [Export]
v1        archived   2026-01-01T00:00Z  system       [Export]

[Rotate Key]  (generates v6)
```

- **Version** — sequential version number
- **Status** — active (current), retired (old), archived (very old)
- **Created** — when version was created
- **Rotated By** — system or admin name who initiated rotation
- **Actions** — Export, Use (set active), Delete

**Tab: Safety Limits**

```
Key Name: [master-key-2026 ▼]

AEAD Safety Limits:

Encryptions Performed:  450,234 / 2^32 (limit)  [🟢 Safe]
Data Encrypted:         12.3 GB / unlimited

Danger Zone:            < 2^30 encryptions remaining

Current Status: ✓ Safe (87% under safety limit)
Last Check:     2026-04-25T14:30:00Z

[Check Now] [Update Limits] [Clear Limits]
```

- Shows current key usage against safety limits
- Status indicator: Safe (green), Warning (yellow), Exceeded (red)
- Buttons to update or clear limits

**Tab: Policies**

```
Key Name: [master-key-2026 ▼]

Key Policy (JSON):

{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    }
  ]
}

[Edit] [Save] [Reset to Default]
```

- Raw JSON policy editor for fine-grained access control

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Generate Key | Plus | Opens a form to create a new encryption key | `POST /v1/kms/keys` |
| Rotate Key | Sync | Creates a new version of the key | `POST /v1/kms/keys/{name}/rotate` |
| Set Default | Star | Marks a key as the system default for new encryptions | `PUT /v1/kms/keys/{name}/default` |
| Enable/Disable | Toggle | Toggles key status (enabled = usable, disabled = blocked) | `PUT /v1/kms/keys/{name}/status` |
| Delete Key | Trash | Removes a key (show warning if default or in use) | `DELETE /v1/kms/keys/{name}` |
| Export Key | Download | Exports a key wrapped with a wrapping key (for backup) | `POST /v1/kms/keys/{name}/export` |
| Import/Unwrap | Upload | Imports a previously exported key | `POST /v1/kms/keys/unwrap` |
| Set Safety Limits | Edit | Configures AEAD safety thresholds | `PUT /v1/kms/keys/{name}/safety` |
| Update Policy | Save | Saves key access control policy | `PUT /v1/kms/keys/{name}/policy` |

### Walkthrough

#### Generate your first encryption key

1. Open the KMS view (`#/security/kms`).
2. Click the **Keys** tab.
3. Click **+ Generate Key** button.
4. A form modal appears:

```
Generate Encryption Key

Key Name:       [production-dek______________]  (DEK = Data Encryption Key)
Key Type:       [AES-256 ▼]  (or RSA-2048, RSA-4096, ED25519, etc.)
Rotation Policy: [Annual ▼]  (or Never, Quarterly, Monthly, On-Demand)

Description:    [Production data encryption key_____________]
                (optional)

Set as Default: [✓] (checkbox; makes this the system default)

[Cancel] [Generate]
```

5. Enter **Key Name**: `production-dek`.
6. **Key Type** defaults to "AES-256"; leave it (good for data encryption).
7. **Rotation Policy**: Set to "Annual" (rotate once per year).
8. **Description**: `Production data encryption key`.
9. Check **Set as Default** to use this key for new encryptions.
10. Click **Generate**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Key generated: production-dek"
✓ Key appears in the Keys table with status "enabled" (green)
✓ Star icon (★) shows this is the default key
```

#### Rotate a key

11. Find your key in the Keys table.
12. Click the three-dot menu and select **Rotate Key**.
13. A confirm modal appears:

```
Rotate Key: production-dek?

A new version will be created and marked active.
Old versions will be archived after 90 days.

[Cancel] [Rotate]
```

14. Click **Rotate**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Key rotated: production-dek"
✓ Key status remains "enabled" (green)
```

15. Click the **Key Versions** tab and select "production-dek" from the dropdown.
16. You now see two versions:

```
Version   Status     Created
──────────────────────────────
v2        active     2026-04-25T14:35Z   (newly created)
v1        retired    2026-04-20T12:00Z   (old version)
```

#### Set a default key

17. Click the **Keys** tab.
18. Create a second key (e.g., `backup-dek`) by clicking **+ Generate Key** and repeating the steps but **unchecking** "Set as Default".
19. The new key appears with no star icon.
20. Click the three-dot menu and select **Set Default**.
21. A confirm modal appears:

```
Set Default Key?

backup-dek will become the system default.
All new encryptions will use this key.

[Cancel] [Set Default]
```

22. Click **Set Default**.

**Expected outcome:**

```
✓ Success toast: "Default key changed to backup-dek"
✓ Star icon moves from production-dek to backup-dek
✓ production-dek status remains "enabled" (can still decrypt old data)
```

#### Check and update safety limits

23. Click the **Safety Limits** tab.
24. **Key Name** should show "production-dek" (or select from dropdown).
25. You see current usage:

```
Encryptions Performed:  1,234 / 2^32  (very small percentage)
Data Encrypted:         2.3 GB / unlimited
Current Status: ✓ Safe (99.99% under limit)
```

26. Click **Update Limits** to adjust thresholds if needed (e.g., lower the encryption count limit for more frequent rotation).
27. A form modal appears:

```
Update Safety Limits: production-dek

Max Encryptions:  [2^32 ▼]  (or custom limit)
Max Data Volume:  [Unlimited ▼]  (or custom limit in GB)
Alert Threshold:  [90% ▼]  (alert when usage exceeds this)

[Cancel] [Save]
```

28. Leave defaults for now. Click **Save**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Safety limits updated"
```

#### Set a key policy

29. Click the **Policies** tab.
30. **Key Name** shows "production-dek" (or select from dropdown).
31. You see a JSON policy editor:

```json
{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    }
  ]
}
```

32. Modify the policy to restrict decryption to admins only (keep encryption open):

```json
{
  "version": "2012-10-17",
  "statement": [
    {
      "effect": "allow",
      "principal": "role:admin",
      "action": ["kms:decrypt", "kms:encrypt"],
      "resource": "*"
    },
    {
      "effect": "allow",
      "principal": "role:user",
      "action": ["kms:encrypt"],
      "resource": "*"
    }
  ]
}
```

33. Click **Save**.

**Expected outcome:**

```
✓ Success toast: "Policy updated"
✓ Users with role:user can now encrypt but not decrypt
✓ Only role:admin can do both
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Generate Key form shows validation error | Key name is empty or already exists | Choose a unique key name |
| Rotate fails with "Key cannot be rotated" | Rotation policy is set to "Never" | Update the rotation policy or rotate manually |
| Set Default fails | Key is disabled | Enable the key first; then set as default |
| Delete fails with "Key is in use" | Key is the default or currently encrypting data | Set a different key as default; wait for pending operations to complete |
| Safety limits show "Exceeded" (red) | AEAD operation count has reached the limit | Rotate the key immediately; update limits if needed |
| Policy JSON shows parse error | JSON syntax is invalid | Fix curly braces, quotes, commas; use a JSON validator |
| Export fails | Key export is not allowed by policy | Check key policy; ensure your role has kms:export permission |

### See also

- [CLI: quicbaas kms](../kms/cli.md) — manage KMS via CLI
- [handlers_kms.go](../../pkg/api/rest/handlers_kms.go) — KMS implementation
- [security/kms.md](../kms/architecture.md) — KMS architecture and design
- [security/kms-multi-tenancy.md](../multi-tenancy/engineering.md) — multi-tenant KMS
- Security → Secrets — secret management
- Security → Seal — database seal and recovery keys


---

## Security → Seal

### Purpose

The Seal view initializes the database seal, unseals the database, manages recovery keys, and rotates the root encryption key. Use this view during initial setup, after system recovery, and for key rotation ceremonies. The seal is the fundamental mechanism that protects the database's master encryption key.

### Layout

The page displays the current seal status and management controls:

**Status Section** (top)

```
Seal Status:           🔒 SEALED  (or 🔓 UNSEALED)
Initialized:           ✓ Yes (or ⚠ No — first-time setup required)
Master Key Status:     ✓ Protected (or ⚠ Compromised — rekey required)
Recovery Keys:         ✓ Stored securely
Last Rotation:         2026-01-15T08:00Z (or Never)

[Unseal] (if sealed) [Seal] (if unsealed) [Rekey] [Rotate Root Key]
```

- **Seal Status** — SEALED (red lock) or UNSEALED (green unlock)
- **Initialized** — whether seal has been initialized
- **Master Key Status** — Protected or Compromised (affects available actions)
- **Recovery Keys** — whether recovery keys exist
- **Last Rotation** — timestamp of last root key rotation

**If Status is "Not Initialized"** — shows:

```
Seal not initialized.

This is required for the first-time setup. Choose a seal backend:

Seal Backend:  [Filesystem ▼]  (or Vault, CloudKMS, YubiHSM, etc.)
               [Configure...]

[Initialize Seal]
```

**If Status is "Sealed"** — shows Unseal form:

```
Unseal Database

Seal Backend:    [Filesystem]  (read-only; shows configured backend)
Unseal Key:      [••••••••••________] or [Choose File...]
                 (depending on backend; may be a key share, password, or file)

Additional Shares: (if Shamir is configured)
  Share 1: [••••••••••________]
  Share 2: [••••••••••________]
  Share 3: [••••••••••________]
  (Need 3 of 5 shares entered)

                 [Unseal]
```

**Tab: Recovery Keys** (or expandable section)

```
Recovery Keys:

Stored Locally: ✓ Yes
Last Generated: 2026-01-15T08:00Z
Key Count:      1 (single recovery key)

Key ID               Generated             Status    Actions
──────────────────────────────────────────────────────────
rec_abc123def456     2026-01-15T08:00Z     active    [Download] [Rotate] [Delete]

[Generate New Recovery Key]
```

- Lists recovery keys (used for emergency recovery)
- Options to generate, rotate, or delete

**Tab: Root Key** (or expandable section)

```
Root Key Rotation:

Current Root Key:   rk_2026-01-15_v1
Created:            2026-01-15T08:00Z
Age:                3 months 10 days
Recommended Age:    1 year (rotate annually)

Status: ✓ Healthy (no rotation needed yet)

[Rotate Root Key]
```

- Shows current root key age and rotation status

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Initialize Seal | Lock | Initializes the seal with a backend (first-time only) | `POST /v1/seal/init` |
| Unseal Database | Unlock | Unseals with key shares or password (if sealed) | `POST /v1/seal/unseal` |
| Seal Database | Lock | Seals the database (only if unsealed) | `POST /v1/seal/seal` |
| Rekey | Refresh | Changes the master key by splitting it into new shares | `POST /v1/seal/rekey` |
| Rotate Root Key | Sync | Rotates the root encryption key (long-term protection) | `POST /v1/seal/rotate-root` |
| Generate Recovery Key | Plus | Creates a new emergency recovery key | `POST /v1/seal/recovery/generate` |
| Delete Recovery Key | Trash | Removes a recovery key (shows warning) | `DELETE /v1/seal/recovery` |

### Walkthrough

#### First-time initialization

1. Open the Seal view (`#/security/seal`).
2. If the view shows "Seal not initialized", you're in first-time setup mode.
3. **Seal Backend** dropdown shows options:

```
[Filesystem ▼]
  Filesystem (local file in data-dir/seal/)
[Vault ▼]
  HashiCorp Vault (external KMS)
[CloudKMS ▼]
  Google Cloud KMS
[YubiHSM ▼]
  Yubico YubiHSM 2
[AzureKeyVault ▼]
  Azure Key Vault
```

4. For testing, select **Filesystem**.
5. Click **[Configure...]** to set options (e.g., number of key shares, threshold).
6. A modal appears:

```
Configure Seal Backend: Filesystem

Key Shares:      [5]  (total number of shares)
Threshold:       [3]  (shares needed to unseal)
Location:        [/data/seal/keys]  (auto-filled)

[Confirm]
```

7. Leave defaults (5 shares, 3 threshold — need 3 of 5 to unseal).
8. Click **[Confirm]**.

**Expected outcome:**

```
✓ Modal closes
✓ "Initialize Seal" button becomes available
```

9. Click **[Initialize Seal]**.
10. A multi-step modal appears:

```
Step 1: Initialize

Initializing seal with Filesystem backend...
5 key shares will be generated.
You need 3 shares to unseal.

[Next] [Cancel]
```

11. Click **[Next]**.
12. Step 2 shows the generated key shares (write them down or save them securely):

```
Step 2: Save Key Shares

IMPORTANT: Save these key shares in a secure location.
Each share should be stored separately.

Share 1: U8d7f9K2j4H6g5L9N3Q8w2E5R6T7Y8U9I...
Share 2: K4m1O7p9L2j3H5g6F8d2E4R9T7Y6U5I3Q...
Share 3: ...
Share 4: ...
Share 5: ...

[✓] I have saved all 5 shares
[Next] [Cancel]
```

13. Copy and save each share to a secure location (password manager, hardware storage, etc.).
14. Check the "I have saved..." checkbox.
15. Click **[Next]**.
16. Step 3 generates and shows the recovery key:

```
Step 3: Recovery Key

Save this recovery key for emergency recovery.

Recovery Key: rec_abc123def456_XXXXXX...

[Download] [Copy]

[✓] I have saved the recovery key
[Next] [Cancel]
```

17. Save the recovery key securely.
18. Check the checkbox and click **[Next]**.
19. Step 4 confirms:

```
Step 4: Complete

Seal has been initialized and the database is now UNSEALED.

Status: 🔓 UNSEALED

Important next steps:
1. Rotate or delete the recovery key monthly for operational security
2. Back up your key shares in separate, secure locations
3. Test the unseal process (seal, then unseal)

[Done]
```

20. Click **[Done]**.

**Expected outcome:**

```
✓ Seal Status shows: 🔓 UNSEALED
✓ "Initialized" shows: ✓ Yes
```

#### Seal and unseal the database

21. After initialization, click **[Seal]** to seal the database (optional; for testing).
22. A confirm modal appears:

```
Seal the Database?

The database will be locked. You will need key shares to unseal it.

[Cancel] [Seal]
```

23. Click **[Seal]**.

**Expected outcome:**

```
✓ Status immediately shows: 🔒 SEALED
✓ All Unseal form fields appear
```

24. Click **[Unseal]** to unseal it again (demonstrate the process).
25. An Unseal form appears:

```
Unseal Database

Share 1: [••••••••••________]  (paste first key share)
Share 2: [••••••••••________]  (paste second key share)
Share 3: [••••••••••________]  (paste third key share)

[Clear] [Unseal]
```

26. Paste 3 of your saved key shares.
27. Click **[Unseal]**.

**Expected outcome:**

```
✓ Status updates: 🔓 UNSEALED
✓ Success toast: "Database unsealed"
```

#### Rotate the root key

28. Click the **Root Key** tab (or scroll down).
29. You see:

```
Current Root Key:   rk_2026-01-15_v1
Created:            2026-01-15T08:00Z
Age:                3 months 10 days
Status: ✓ Healthy
```

30. Once per year (or after a security incident), click **[Rotate Root Key]**.
31. A confirm modal appears:

```
Rotate Root Encryption Key?

A new root key will be generated and the old key will be retired.
This operation takes 1-5 minutes depending on data size.

[Cancel] [Rotate]
```

32. Click **[Rotate]**.

**Expected outcome:**

```
✓ Progress bar shows rotation in progress
✓ After completion, success toast: "Root key rotated"
✓ Current Root Key updates to new version and timestamp
✓ Age resets to 0 days
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| "Seal is already initialized" | Seal was already set up previously | Skip initialization; go directly to Unseal if sealed |
| Unseal fails with "Invalid key share" | Key share is incorrect or corrupted | Verify the share is correct; re-copy from secure storage |
| Unseal requires more shares than expected | Configured threshold is higher than shares entered | Enter the correct number of shares; check configuration |
| "Master Key Status: Compromised" appears | Indicates a security issue | Immediately rekey or rotate root key; review security logs |
| Rekey fails | Master key is compromised; rekey is locked | Restore from backup or contact support |
| Root key rotation is slow | Large database or slow disk I/O | This is normal; wait for completion; check disk health |
| Recovery key cannot be generated | Backend does not support recovery keys | Some backends (YubiHSM) have limited recovery support; check documentation |
| Delete recovery key fails | Recovery key is in use or being validated | Wait and try again; or regenerate a new recovery key first |

### See also

- [CLI: quicbaas seal](../barrier/cli.md) — seal operations via CLI
- [handlers_seal.go](../../pkg/api/rest/handlers_seal.go) — seal implementation
- [security/seal.md](../barrier/engineering.md) — seal architecture and best practices
- Security → KMS — key management service
- [security/kms.md](../kms/architecture.md) — encryption key management


---

## Security → Secrets

### Purpose

The Secrets view stores, rotates, and versions sensitive data (API keys, passwords, credentials) with automatic encryption via the KMS. Use this view to create secrets, retrieve them, rotate sensitive values, and manage secret versions and metadata.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "Secrets", "Versions", "Audit", "Settings"

**Tab: Secrets**

```
Search: [________]  (by secret path or name)

Create Secret: [+ New Secret]

Secrets:

Path                         Type       Status    Rotated    Last Access      Actions
────────────────────────────────────────────────────────────────────────────────────
db/prod/password             string     enabled   2026-04-20 2026-04-25T14:00Z [⋮]
api-keys/github              string     enabled   Never      2026-04-25T13:00Z [⋮]
tls/ca-cert                  certificate enabled  Never     2026-01-15T10:00Z [⋮]
oauth/client-secret          string     disabled  2026-02-28 —                 [⋮]

[View] [Rotate] [Enable/Disable] [Delete]  (per secret)
```

- **Path** — hierarchical identifier (e.g., `db/prod/password`)
- **Type** — string, certificate, key-pair, json, etc.
- **Status** — enabled (green), disabled (gray)
- **Rotated** — last rotation date or "Never"
- **Last Access** — ISO 8601 timestamp or "—"
- Three-dot menu for actions

**Tab: Versions**

```
Secret Path: [db/prod/password ▼]  (dropdown to select secret)

Versions:

Version   Created            Rotated By   Status     Actions
───────────────────────────────────────────────────────────
v3        2026-04-20T08:00Z  admin        current    [Use as Current] [Delete]
v2        2026-03-20T08:00Z  system       retired    [Use as Current] [Delete]
v1        2026-02-20T08:00Z  admin        archived   [Delete]

[Rollback] (to a previous version)
```

- **Version** — sequential version number
- **Created** — when version was created
- **Rotated By** — who initiated rotation (system or admin name)
- **Status** — current (in use), retired (old), archived (very old)
- **Actions** — Use as Current (rollback), Delete

**Tab: Audit**

```
Secret Path: [db/prod/password ▼]

Audit Log (read-only):

Timestamp              Action          User           Result
──────────────────────────────────────────────────────────────
2026-04-25T14:00:00Z   read           admin          ✓ Success
2026-04-25T13:30:00Z   rotate         system         ✓ Success
2026-04-25T12:00:00Z   read           client-app     ✓ Success
2026-04-20T08:00:00Z   create         admin          ✓ Success

[Export Audit Log] [Filter by Action]
```

- **Timestamp** — ISO 8601 timestamp
- **Action** — create, read, rotate, delete, enable, disable
- **User** — who performed the action (admin, system, client app name)
- **Result** — Success (green), Failed (red)

**Tab: Settings**

```
Secrets Configuration:

Encryption Key:       [production-dek ▼]  (KMS key used for encryption)
Rotation Policy:      [90 days ▼]  (recommend or enforce rotation interval)
Audit Logging:        [✓] Enabled
Max Secret Size:      [1 MB ▼]
Automatic Rotation:   [Disabled ▼]  (or Enabled with interval)

Access Control:
  Default TTL:        [1 hour ▼]  (how long secret is cached in memory)
  Require MFA:        [Disabled ▼]  (for sensitive secrets)

                      [Save Settings]
```

- Global configuration for secret management

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Secret | Plus | Opens a form to store a new secret | `POST /v1/secrets/{path}` |
| Retrieve Secret | Eye | Displays the secret value (masked by default) | `GET /v1/secrets/{path}` |
| Rotate Secret | Sync | Creates a new version; optionally invalidates old ones | `POST /v1/secrets/{path}/rotate` |
| List Secrets | List | Lists all secrets (path only; values hidden) | `GET /v1/secrets` |
| List Versions | History | Shows all versions of a secret | `GET /v1/secrets/{path}/versions` |
| Rollback | Undo | Activates a previous version as current | `POST /v1/secrets/{path}/rollback` |
| Enable/Disable | Toggle | Toggles secret access (disabled = blocked) | `POST /v1/secrets/{path}/enable` or `/disable` |
| Delete Secret | Trash | Removes all versions of a secret; show confirm modal | `DELETE /v1/secrets/{path}` |
| Export Audit Log | Download | Downloads audit entries as CSV | — (client-side export) |

### Walkthrough

#### Create your first secret

1. Open the Secrets view (`#/security/secrets`).
2. Click the **Secrets** tab.
3. Click **+ New Secret** button.
4. A form modal appears:

```
Create Secret

Path:         [db/prod/password________________]  (hierarchical; use / separator)
Type:         [String ▼]  (or Certificate, Key-Pair, JSON, etc.)

Secret Value: [••••••••••] (password input; masked)
              [Show] (toggle to reveal)

Metadata:
  Description: [Production database password_____]  (optional)
  Tags:        [env:prod, app:api__________]  (optional)
  Auto-Rotate: [Enabled ▼] Every [90 days]

Encryption:   [production-dek ▼]  (KMS key to use)

              [Cancel] [Create]
```

5. Enter **Path**: `db/prod/password`.
6. **Type** defaults to "String"; leave it.
7. In **Secret Value**, enter a password (e.g., a database password or use a generated one).
8. Click **[Show]** to verify it's correct.
9. **Description**: `Production database password`.
10. **Tags**: `env:prod,app:api`.
11. **Auto-Rotate**: Leave "Enabled" with "90 days" (automatic rotation every 90 days).
12. **Encryption**: Defaults to "production-dek" (your KMS key); leave it.
13. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Secret created: db/prod/password"
✓ Secret appears in the Secrets table with status "enabled" (green)
✓ "Rotated" shows "Never" (new secret, not yet rotated)
```

#### Retrieve a secret

14. Find your secret in the table and click its row.
15. A detail panel opens showing:

```
Secret: db/prod/password

Path: db/prod/password
Type: string
Status: enabled
Created: 2026-04-25T14:35:00Z
Description: Production database password
Tags: env:prod, app:api

Secret Value: [••••••••••] (masked)
              [Show] [Copy] [Use in Prompt]

Versions: 1

[Rotate] [Disable] [Delete]
```

16. Click **[Show]** to reveal the secret value (may require MFA if configured).

**Expected outcome:**

```
✓ Secret value is displayed (unmasked)
✓ [Show] toggles back to [Hide]
```

17. Click **[Copy]** to copy to clipboard (useful for pasting into configs).

#### Rotate a secret

18. From the secret detail panel, click **[Rotate]**.
19. A form modal appears:

```
Rotate Secret: db/prod/password

New Value: [••••••••••]  (or auto-generate)
           [Generate] [Show]

Invalidate Old Versions: [✓] (checkbox; retire versions after rotation)
  Retire After: [7 days ▼]  (grace period before old version is archived)

[Cancel] [Rotate]
```

20. Enter a new password or click **[Generate]** to create a random one.
21. Check **Invalidate Old Versions** to retire the old password (good practice).
22. Set "Retire After" to "7 days" (grace period for clients to update config).
23. Click **[Rotate]**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Secret rotated: db/prod/password"
✓ Secrets table updates: "Rotated" shows today's date
```

24. Click the **Versions** tab and select `db/prod/password`.
25. You now see two versions:

```
Version   Created            Rotated By   Status
───────────────────────────────────────────────
v2        2026-04-25T14:40Z  admin        current
v1        2026-04-25T14:35Z  admin        retired (will auto-archive in 7d)
```

#### Rollback to a previous version

26. If you made a mistake and need to revert, click on version v1 in the Versions list.
27. Click **[Use as Current]**.
28. A confirm modal appears:

```
Rollback to version v1?

The old password will be activated. Systems using the new version may break.

[Cancel] [Rollback]
```

29. Click **[Rollback]**.

**Expected outcome:**

```
✓ Success toast: "Rolled back to v1"
✓ Status badge changes: v1 → current, v2 → retired
```

#### Check audit logs

30. Click the **Audit** tab.
31. **Secret Path** shows `db/prod/password`.
32. The audit log displays all actions on this secret:

```
Timestamp              Action        User     Result
─────────────────────────────────────────────────────
2026-04-25T14:42:00Z   rollback      admin    ✓ Success
2026-04-25T14:40:00Z   rotate        admin    ✓ Success
2026-04-25T14:35:00Z   create        admin    ✓ Success
```

33. Click **[Export Audit Log]** to download audit entries as CSV.

**Expected outcome:**

```
✓ Browser downloads: secrets-audit-db-prod-password-2026-04-25.csv
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Secret form shows validation error | Path is empty or invalid | Use a valid path like `db/prod/password`; avoid spaces |
| Cannot reveal secret | MFA is required but not set up | Skip [Show] or enable MFA and log in |
| Rotate fails with "Cannot auto-generate password" | Type is not a string (e.g., certificate) | Generate the new value manually; or use `[Generate]` for compatible types |
| Rollback fails with "Only one version" | Secret has only one version; cannot rollback | You cannot rollback a secret with no history |
| Delete confirms but secret still appears | Audit cache is stale | Refresh the page |
| Auto-rotation does not trigger | Automatic rotation is disabled in settings | Enable auto-rotation in the Settings tab |
| Secret size exceeds 1 MB | Value is too large for the configured limit | Compress the value; or increase max size in Settings (may require restart) |

### See also

- [CLI: quicbaas secrets](../secrets/cli.md) — manage secrets via CLI
- [handlers_secrets.go](../../pkg/api/rest/handlers_secrets.go) — secrets implementation
- [security/secrets.md](../secrets/engineering.md) — secrets architecture and design
- [security/audit.md](../audit-logging/engineering.md) — audit logging
- Security → KMS — key management (encrypt secrets)


---

## Security → SSH

### Purpose

The SSH view manages SSH keypairs and known hosts for secure shell access. Use this view to generate new SSH keys, import existing keys, view public keys for authorized_keys files, and maintain a registry of trusted host fingerprints. SSH integration supports agent-based operations and remote deployments.

### Layout

The page is structured as a tabbed interface:

**Tabs** — "SSH Keys", "Known Hosts"

**Tab: SSH Keys**

```
Scope:   [Global ▼]  (or User, Organization, etc.)
Owner:   [________] (filter by owner; optional)

Create Key: [+ Generate Key] [+ Import Key]

SSH Keys:

Name                 Type        Key Size  Owner       Created            Actions
────────────────────────────────────────────────────────────────────────────────
deploy-key-prod      RSA         4096      admin       2026-04-20T08:00Z  [⋮]
github-deploy        ED25519     256       admin       2026-04-15T14:00Z  [⋮]
user-laptop          RSA         2048      alice       2026-03-01T10:00Z  [⋮]

[Export Public Key] [Export Private Key] [Delete]  (per key)
```

- **Name** — unique key identifier
- **Type** — RSA, ED25519, ECDSA, etc.
- **Key Size** — bit length (2048, 4096, 256, etc.)
- **Owner** — user or role that owns the key
- **Created** — ISO 8601 timestamp
- Three-dot menu for actions

**Tab: Known Hosts**

```
Add Host:  [+ Add Known Host]

Known Hosts:

Host             Port  Key Type  Fingerprint (SHA256)                     Added      Actions
─────────────────────────────────────────────────────────────────────────────────────────────
github.com       22    RSA       SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...  2026-01-15 [⋮]
gitlab.com       22    ECDSA     SHA256:HbW3g6CQhzrPCfx9L8mLa7TqY3E5vT9jN...       2026-02-20 [⋮]
internal-git     22    ED25519   SHA256:aBcD1EfG2hIj3KlM4nOpQrStUvWxYzA5...       2026-04-10 [⋮]

[Verify] [Delete]  (per host)
```

- **Host** — hostname or IP address
- **Port** — SSH port (usually 22)
- **Key Type** — host key algorithm
- **Fingerprint** — SHA256 hash of the public key for verification
- **Added** — when this host was pinned
- Three-dot menu for actions

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Generate Key | Plus | Creates a new SSH keypair | `POST /ssh/keys/{scope}/{owner}/generate` |
| Import Key | Upload | Imports a PEM-encoded private key | `POST /ssh/keys/{scope}/{owner}/import` |
| Export Public Key | Download | Shows the public key in authorized_keys format | `GET /ssh/keys/{scope}/{owner}/{name}/public` |
| Export Private Key | Download | Downloads the private key (PEM-encoded, encrypted) | `GET /ssh/keys/{scope}/{owner}/{name}/private` |
| Delete Key | Trash | Removes a keypair; show warning if in use | `DELETE /ssh/keys/{scope}/{owner}/{name}` |
| Add Known Host | Plus | Pins a host's public key fingerprint | `POST /known-hosts` |
| Verify Host | Check | Verifies the stored fingerprint against the current host key | `POST /known-hosts/{host}/verify` |
| Delete Known Host | Trash | Removes a pinned host from the known hosts registry | `DELETE /known-hosts/{host}` |

### Walkthrough

#### Generate your first SSH keypair

1. Open the SSH view (`#/security/ssh`).
2. Click the **SSH Keys** tab.
3. Click **+ Generate Key** button.
4. A form modal appears:

```
Generate SSH Keypair

Name:           [deploy-prod______________]
Type:           [RSA ▼]  (or ED25519, ECDSA)
Key Size:       [4096 ▼]  (2048, 3072, 4096 for RSA; 256 for ED25519)
Owner:          [admin__________]  (current user or role)
Passphrase:     [••••••••••________]  (optional; protects private key)
                [Generate passphrase] or [Leave empty]

Description:    [Production deployment key__________]  (optional)

[Cancel] [Generate]
```

5. Enter **Name**: `deploy-prod`.
6. **Type**: Select "RSA" (most compatible).
7. **Key Size**: Select "4096" (strong encryption).
8. **Owner**: Defaults to current user; leave as-is.
9. **Passphrase**: Optional. For testing, leave empty. In production, set a strong passphrase.
10. **Description**: `Production deployment key`.
11. Click **[Generate]**.

**Expected outcome:**

```
✓ Modal shows generation progress
✓ After completion, displays the new keypair summary:

  Key: deploy-prod (RSA-4096)
  Owner: admin
  Fingerprint (SHA256): aBcD1EfG2hIj3KlM4nOpQrStUvWxYzA5bCdEfGhIjK...
  Created: 2026-04-25T14:35:00Z

  ✓ I have securely stored the private key
  [Download Private Key] [Copy Public Key] [Done]
```

12. Click **[Download Private Key]** to save it securely (e.g., `~/.ssh/deploy-prod`).

**Expected outcome:**

```
✓ Browser downloads the PEM-encoded private key
✓ File: deploy-prod (or deploy-prod.pem)
```

13. Click **[Copy Public Key]** to copy the authorized_keys-format public key to clipboard.

**Expected outcome:**

```
✓ Public key copied: ssh-rsa AAAAB3NzaC1yc2EAAA...
✓ Can paste into ~/.ssh/authorized_keys on remote hosts
```

14. Click **[Done]**.

**Expected outcome:**

```
✓ Modal closes
✓ SSH Keys table now shows:
  deploy-prod   RSA      4096     admin      2026-04-25T14:35Z  [⋮]
```

#### Import an existing SSH key

15. Click **+ Import Key**.
16. A form modal appears:

```
Import SSH Key

Name:         [github-deploy______________]
Scope:        [Global ▼]
Owner:        [admin__________]

Private Key:  [Paste PEM-encoded key here...
               -----BEGIN OPENSSH PRIVATE KEY-----
               ...
               -----END OPENSSH PRIVATE KEY-----]
               or [Choose File...]

Passphrase:   [••••••••••________]  (if key is passphrase-protected)

[Cancel] [Import]
```

17. Enter **Name**: `github-deploy`.
18. Paste or upload an existing private key (PEM or OpenSSH format).
19. If the key has a passphrase, enter it in **Passphrase**.
20. Click **[Import]**.

**Expected outcome:**

```
✓ Modal closes
✓ Key appears in the SSH Keys table
✓ Success toast: "Key imported: github-deploy"
```

#### Export a public key

21. Find a key in the SSH Keys table (e.g., `deploy-prod`).
22. Click the three-dot menu and select **Export Public Key**.
23. A modal shows the public key in authorized_keys format:

```
Public Key: deploy-prod (RSA-4096)

ssh-rsa AAAAB3NzaC1yc2EAAA...

[Copy] [Download]
```

24. Click **[Copy]** to copy to clipboard for pasting into remote `~/.ssh/authorized_keys`.

#### Pin a known host

25. Click the **Known Hosts** tab.
26. Click **+ Add Known Host**.
27. A form modal appears:

```
Add Known Host

Host:       [github.com______________]
Port:       [22]  (SSH port, usually 22)

Fetch Fingerprint:  [Fetch from Host]  (auto-detect)
  or
Manual Fingerprint: [SHA256:nThbg6kXUpJWGl7E1IGOCspRomTxdCARLvi...]

[Cancel] [Add]
```

28. Enter **Host**: `github.com`.
29. **Port**: Defaults to 22; leave it.
30. Click **[Fetch from Host]** to auto-detect the host's SSH key fingerprint.

**Expected outcome:**

```
✓ Progress indicator shows "Fetching..."
✓ After 1-2 seconds, auto-populated Fingerprint:
  Key Type: RSA
  SHA256: nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...
```

31. Click **[Add]**.

**Expected outcome:**

```
✓ Modal closes
✓ Host appears in Known Hosts table:
  github.com   22    RSA       SHA256:nThbg6kXUpJWGl7E...   2026-04-25 [⋮]
```

#### Verify a known host

32. Find the known host in the table (e.g., `github.com`).
33. Click the three-dot menu and select **Verify**.
34. A modal appears:

```
Verify Host: github.com

Stored Fingerprint (SHA256):
  nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...

Current Fingerprint (from Host):
  nThbg6kXUpJWGl7E1IGOCspRomTxdCARLviKw...

Status: ✓ MATCH

[Close]
```

**Expected outcome:**

```
✓ Status shows "MATCH" (green checkmark)
✓ The host's current key matches the stored fingerprint
```

35. If they don't match, the status shows "MISMATCH" (red X) — indicates a potential MITM attack; do not proceed.

#### Delete a key or host

36. To delete an SSH key, click the three-dot menu and select **Delete**.
37. A confirm modal appears:

```
Delete SSH Key: deploy-prod?

This cannot be undone. Make sure you have a backup of the private key.

[Cancel] [Delete]
```

38. Click **[Delete]**.

**Expected outcome:**

```
✓ Success toast: "Key deleted: deploy-prod"
✓ Key disappears from the table
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Generate Key button is disabled | Required fields are empty | Fill in Name and Type |
| Import Key fails with "Invalid PEM format" | Key file is not a valid PEM-encoded SSH key | Verify file format; use `ssh-keygen -p -N "" -m pem -f keyfile` to convert |
| Export Private Key fails with "Access denied" | User does not have permission to export | Check IAM role; ensure you have `ssh:export` permission |
| Fingerprint Fetch fails with "Cannot connect to host" | Host is unreachable or SSH port is not 22 | Verify hostname/IP and port; check network connectivity |
| Verify Host shows "MISMATCH" | Host key has changed (normal after server updates) | Update the stored fingerprint by deleting and re-adding the host |
| Cannot delete a key | Key is in use by an active session or agent | Revoke the key's use before deleting; or wait for sessions to close |

### Keyboard Shortcuts

- None specific to SSH view (uses standard copy, paste, download)

### Accessibility Notes

- SSH key values are masked by default for security; use [Show] toggle to reveal
- Copy buttons have aria-labels for screen readers
- Fingerprint comparison view includes color-coded status (green/red) with text labels for colorblind users

### See also

- [CLI: quicbaas ssh](../ssh/cli.md) — manage SSH keys via CLI
- [handlers_ssh.go](../../pkg/api/rest/handlers_ssh.go) — SSH key implementation
- [agents/deploy.md](../software-factory/cli.md) — SSH integration for agent deployment
- security/authentication.md — authentication mechanisms


---

## Software Factory → Cost Tracking

### Purpose

Cost Tracking reports what the tenant has actually SPENT, read from the
consumption ledger — one durable record per provider turn, written by the same
path the spend cap is enforced from. Use it to see which projects, tasks and
jobs consumed tokens, which pooled account paid, and how much of that
account's configured cap is left.

It is NOT an estimator. Every figure on the page is either a measurement the
ledger holds or is shown as a dash and labelled; see "What the page will not
claim" below.

### Layout

**Header** — "Cost Tracking", with a breadcrumb showing where in the
attribution chain the report is taken: `Tenant › project › task › job`.

**Range** — All time, or the last 24 hours, 7 days or 30 days. Changing it
reloads the report for the same scope.

**Money** — the list-price equivalent, what was billed (zero per turn on a
subscription) and the energy local inference drew. At the tenant level the
billed card also shows the subscription fees pro-rated over the range.

**Volume** — Total tokens, Input (with cache reads and writes), Output and
Turns (`N ok · M failed`).

**By &lt;level&gt;** — the drill-down: one row per identifier at the next chain
level below the scope, with that subtree's tokens. Clicking a row narrows the
scope; clicking a breadcrumb widens it again. Spend that belongs to no entity
at a level ("a job with no task") is its own row, labelled `No task`.

**Accounts that paid** — per pooled account: the tokens it was charged, its
billing, both money figures, its fee share over the range (tenant level, for a
subscription with a fee), its spend-policy status, each configured quota
window with live usage against it, and the circuit-breaker verdict when one is
configured.

**By model** — tokens and both money figures per engine/model pair.

### What the page will not claim

| Figure | What you see, and why |
|---|---|
| Cost of an unpriced model | A dash, and "no price for these models". A turn on a model the price table does not name is UNPRICED, not free; beside priced turns the figure is a floor ("at least — N turns unpriced"). |
| A subscription fee | A dash unless the account is a subscription with a configured fee, and only at the tenant level. |
| An account with no cap | "No spend policy" — a fact, not a budget of 0%. |
| An account the server could not read | "Spend policy unknown" — a different fact, never collapsed into the reassuring one. |
| Iteration budget | Absent. Only the `workflow` runner iterates, and its budget is on the job page, not here; it was never a cost. |
| Nothing recorded at all | "This deployment is not recording provider consumption" — a deployment with no ledger wired says so instead of showing an empty report. |

### Lifecycle actions

| Action | What it does | Backend endpoint |
|--------|-------------|-----------------|
| Drill into a row | Narrows the scope to that project / task / job / session | `GET /api/v1/orchestrator/cost?project_id=…` |
| Click a breadcrumb | Widens the scope back to that level | `GET /api/v1/orchestrator/cost` |
| Choose a range | Narrows the report to the last day, week or 30 days | `GET /api/v1/orchestrator/cost?from=…` |

### Walkthrough

#### Find what a factory job cost

1. Open Cost Tracking (`#/cost`). It loads the tenant roll-up:

```
Total tokens   Input    Output   Turns            Cost
412,880        330,100  82,780   58 ok · 2 failed  —
                                                   no price sheet — cost unmeasured
```

2. Under **By project**, click the project the job belongs to. The breadcrumb
   becomes `Tenant › <project>` and the breakdown becomes **By task**.
3. Click the task, then the job. The scope is now that single job, and the
   **Accounts that paid** panel names the pooled account it drew on:

```
alice-work   claude   No spend policy                     5,200 tokens
             No spend cap is configured for this account, so no budget can be shown.
```

4. If the account HAS a cap, the same panel shows the live window instead:

```
alice-work   claude   Metered                             5,200 tokens
  five-hour                          5,200 / 1,000,000 tokens per 5h
  [=                                                              ]
```

#### Read one job's spend from the job page

1. Open a job (`#/jobs/{id}`) and select the **Cost** tab. It reads that job's
   ledger entries: total, input, output, the accounts that paid, and the cost
   as a dash while nothing prices a turn.
2. A job driven by the `workflow` runner also shows its iteration gauge. A job
   driven by any other runner says so in place — "The sdlc runner does not
   iterate, so this job has no iteration budget." — rather than showing a
   gauge at 0%. The **DAG** tab stays the workflow runner's; **Cost** is
   offered for both, because every runner spends tokens.

### Troubleshooting

| Message | Cause | Fix |
|-------|-------|-----|
| "This deployment is not recording provider consumption" | No consumption ledger is wired on the daemon | The ledger is built alongside the orchestration KV; check the daemon started its orchestrator |
| "You do not have permission to view spend" | The account lacks `orchestrator:status` | Grant the orchestrator read family |
| Cost is a dash on every row | Expected. Nothing prices a turn yet | Tokens are the measured axis; a currency figure arrives with the price sheet (go-quicbaas#791) |
| A drill-down row says "No task" | Real spend that belongs to no task — a job started outside one | Click it to open that subtree |
| A scope returns "cannot be scoped while a level above it is not" | The chain is a path; a task was named without its project | Drill down from the tenant rather than deep-linking a partial scope |

### See also

- Software Factory → Jobs — monitor and manage jobs
- [Cost enforcement](../accounts/architecture.md#spend-limits) — the ledger, the spend cap and the breaker
- [CLI: quicbaas orchestrator jobs list](../orchestration/cli.md#orchestration-jobs) — jobs via CLI


---

## Software Factory → Live Events

### Purpose

The Live Events view streams job and session events in real-time using Server-Sent Events (SSE). Use this view to monitor the activity of running jobs, sessions, and orchestration operations as they happen. Events include job state changes, session prompts, task claims, and errors.

### Layout

The page displays a live event log with two sections:

**Header** — "Live Events" title with "Real-time orchestration activity stream" subtitle. Controls:

- **Pause/Resume** button — pause the event stream (live events stop appearing)
- **Clear** button — clear all events from the current view (does not affect the daemon's event log)
- **Auto-scroll** toggle — enabled by default; disables when user scrolls up to read old events

**Event Stream** — Vertically stacked cards or rows, newest at the top:

```
[Timestamp] [Event Type] [Status Badge] [Details]

14:32:01    job_started          ✓       Job a1b2c3d4 started on backend claude
14:31:55    session_spawned      ✓       Session xyz789 opened
14:31:50    task_claimed        ✓       Task tsk_123 claimed by agent-01
14:31:45    inference_completed  ✓       Inference finished in 2.34s
14:31:40    job_escalated        ⚠       Job needs human approval
```

Each event shows:

- **Timestamp** — ISO 8601 or relative time (e.g., "5 seconds ago")
- **Event Type** — a fixed set of event kinds (job_started, session_spawned, task_claimed, etc.)
- **Status badge** — checkmark (success), warning triangle, or X (error)
- **Details** — human-readable description of what happened

**Empty State** (on first load):

```
Waiting for events...

Events from jobs, sessions, and tasks will appear here as they occur.
```

**Error State** (if stream disconnects):

```
Event stream disconnected.

The connection was lost. Check your network and refresh the page.

[Refresh] [View Logs]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Pause Stream | Pause | Pauses incoming events (UI does not refresh) | — (client-side only) |
| Resume Stream | Play | Resumes listening for events | — (client-side only) |
| Clear Log | Trash | Clears all events from the current view | — (client-side only) |
| Click Event | ChevronRight | Expands event details; may navigate to related resource | `GET /api/v1/orchestrator/{resource}/{id}` |

### Walkthrough

#### Monitor a job from start to finish

1. Open the Live Events view (`#/orchestration/events`).
2. Ensure the **Auto-scroll** toggle is ON and **Pause** is not active.
3. Open a new browser tab or terminal and start a job:

```bash
quicbaas orchestrator jobs start \
  --backend claude \
  --description "Test job"
```

Or use the GUI: go to Jobs view, click **+ Start Job**, and create one.

4. Return to the Live Events view.
5. You should see events appear in real-time:

```
[14:35:20] job_created       ✓  Job abc123 created
[14:35:21] job_assigned      ✓  Job assigned to agent-01
[14:35:22] job_executing     ✓  Job started execution
[14:35:25] job_escalating    ⚠  Job requires approval (optional)
[14:35:30] job_completed     ✓  Job completed in 10s
```

**Expected outcome:**

```
✓ Events appear as the job progresses
✓ Timestamps increment
✓ Final event shows "job_completed" with green checkmark
```

6. Click one of the job events to expand its details:

```
Job abc123 Completed

State: completed
Duration: 10.23s
Output: {...json payload...}
Backend: claude
Agent: agent-01

[View Full Job] [View Related Events]
```

7. To pause the stream while reading old events, click **Pause**.

**Expected outcome:**

```
✓ New events no longer appear
✓ "Resume" button becomes active
✓ You can scroll up to read history without new events pushing you down
```

8. Click **Resume** to continue listening.

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Event stream disconnected message appears | Network connection lost or daemon is unreachable | Check network; wait for daemon to recover; refresh the page |
| No events appear even though jobs are running | Event stream is paused | Click **Resume** to restart listening |
| Events are delayed by 10+ seconds | Network latency or daemon is overloaded | This is expected under high load; check daemon logs |
| Event details show `[object Object]` instead of readable text | Serialization error in the frontend | Refresh the page; report issue if persists |
| Clicking an event does nothing | Navigation feature not yet implemented | Use the Jobs or Sessions view to see full details |

### See also

- Software Factory → Jobs — monitor and manage jobs
- Software Factory → Sessions — attach to a specific session's event stream
- [handlers_session_events.go](../../pkg/api/rest/handlers_session_events.go) — event stream implementation (REST endpoint: `GET /api/v1/orchestrator/sessions/{sessionID}/events`)
- Software Factory → Workflows — workflows generate events


---

## Software Factory → Fleet

### Purpose

The Fleet view displays registered fleet node daemons and their status. Use this view to enroll new agents, monitor their heartbeat, and decommission offline agents. Agents appear in this registry after either a manual registration (via REST POST) or automatic enrollment (via `quicbaas fleet deploy`).

### Layout

The page displays a searchable, filterable table of agents:

**Header** — "Fleet" title with "Registered agents and their status" subtitle. Primary CTA is **+ Register Agent** button.

**Filters and Search** — Above the table:

- **Status filter dropdown** — "All", "Online", "Offline", "Unknown". Defaults to "All".
- **Search input** — client-side filter on agent name, ID, and hostname.

**Table** — Columns:

- **ID** — unique agent identifier (e.g., `agt_abc123def456`), truncated with tooltip
- **Name** — human-readable agent name (e.g., `gpu-node-01`, `agent-01`)
- **Host** — IP address or hostname where the agent runs
- **Type** — backend type (e.g., `acp`, `ollama`, `opencode`)
- **Status** — badge: "online" (green), "offline" (red), "unknown" (gray)
- **Last Seen** — ISO 8601 timestamp of last heartbeat
- **Actions** — three-dot menu with "Remove" (delete agent from registry)

**Empty State**:

```
No agents registered.
Deploy your first agent using the CLI or API.

[View Deployment Guide button]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Register Agent | Plus | Opens a form to manually register an agent via POST | `POST /api/v1/fleet` |
| Remove Agent | Trash | Unregisters an agent from the fleet; shows a confirm modal | `DELETE /api/v1/fleet/{nodeID}` |
| View Details | ChevronRight | (Not yet implemented; greyed out) Shows agent capabilities and resource usage | `GET /api/v1/fleet/{nodeID}` |

### Walkthrough

#### Deploy an agent using the CLI and verify it appears in the fleet

1. Open the Fleet view (`#/orchestration/agents`).
2. Note the current agent count (likely 0 initially).
3. Open a terminal on a remote machine or use localhost:9999 (QuicBaaS development mode).
4. Deploy an agent using the CLI:

```bash
quicbaas fleet deploy \
  --name my-agent-01 \
  --backend ollama \
  --host localhost \
  --join https://quicbaas.example.com:18080
```

**Expected output:**

```
Agent ID: agt_abc123def456
Status: registered
Name: my-agent-01
Host: localhost
Type: ollama
Last seen: 2026-04-25T17:12:59Z
```

5. Return to the Fleet view in the GUI.
6. Refresh the page (Ctrl+R).
7. Your new agent should appear in the table with status "online" (green).

**Expected outcome:**

```
✓ Agent ID matches the CLI output
✓ Status is "online" (green) if heartbeat is recent
✓ "Last Seen" timestamp is within the last 60 seconds
```

8. To manually remove the agent, click its row's three-dot menu and select **Remove**.
9. A confirm modal appears:

```
Remove agent my-agent-01?

This agent will be unregistered from the fleet. Running jobs will be affected.

[Keep Agent] [Remove Agent]
```

10. Click **Remove Agent**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Agent my-agent-01 removed"
✓ Agent disappears from the table
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Agent shows "offline" (red) immediately after deployment | Agent is not reachable or mTLS cert verification failed | Check fleet node daemon logs: `tail -f agent-logs.txt`; verify CA cert is trusted |
| Agent does not appear in the fleet after 30 seconds | Heartbeat not sent or registration API call failed | Manually register via REST: `curl -X POST https://localhost:18080/api/v1/fleet -d '{"name":"..."}' --cacert ca.crt` |
| Remove button is greyed out | Agent is currently running a job | Wait for the job to complete or cancel it from the Jobs view |
| Register Agent form shows validation error | Required fields are missing (name, backend, host) | Ensure all fields are filled before submitting |

### See also

- [CLI: quicbaas fleet deploy](../agent-fleet/cli.md#fleet-deploy) — deploy flags, preflight and lifecycle
- [CLI: quicbaas fleet list](../agent-fleet/cli.md#registry-verbs) — list fleet nodes via CLI
- Software Factory → Inference — submit prompts to registered fleet nodes
- [Agent fleet user guide](../agent-fleet/user.md) — node bootstrapping runbook


---

## Software Factory → Inference

### Purpose

The Inference view submits stateless prompts to fleet-routed backends (e.g., Ollama) without creating a persistent session. Use this view to test backends, ask quick questions, or run one-off inference tasks. Responses stream in real-time if enabled, and a session history tracks the last 10 calls for reference.

### Layout

The view is structured as four stacked cards:

**Card 1 — Target**

```
Backend:        [Ollama ▼]  (populated from GET /api/v1/orchestrator/backends)
Host Affinity:  [Any ▼]     (filtered from GET /api/v1/fleet)
Model:          [qwen3:1.7b]  (free-text input; defaults to backend default)
```

- **Backend** — dropdown of available backends. Session-only backends (Claude, Codex, OpenCode) are visually disabled with a tooltip: "This backend requires a session. Use Sessions view instead."
- **Host Affinity** — dropdown to pin the prompt to a specific agent. "Any" lets the orchestrator pick the best available.
- **Model** — free-text input; defaults to the backend's default model. Must be installed on the agent.

**Card 2 — Prompt**

```
System Prompt (optional): [_________________________________]
                          [Show / Hide]

Prompt (required):        [_________________________________]
                          [Send] (or press Enter)

⚙️ Stream  [ON | OFF]   (toggle; default ON)
          [Cancel]       (visible when request in-flight)
```

- **System Prompt** — collapsible section for optional system message.
- **Prompt** — required field. Press Enter or click Send.
- **Stream toggle** — enabled by default. When on, response streams as NDJSON chunks. When off, waits for full aggregated response.
- **Cancel button** — visible only during active request.

**Card 3 — Response**

```
Response:

Your response appears here. Stream chunks are appended in real-time.

─────────────────────────────────────────

Model: qwen3:1.7b | Stop Reason: end_turn | Elapsed: 2.34s

[Copy] [Clear]
```

- Live output area with `aria-live="polite"` for screen reader support.
- Footer shows model name, stop reason, and elapsed time.
- **Copy** button — copies response text.
- **Clear** button — resets the response (does not clear the prompt field).

**Card 4 — Session History**

```
Recent Inference Calls (last 10):

14:35:20  [Ollama | qwen3:1.7b | gpu-node-01]
          Q: Explain QUIC
          ⏱️ 2.34s
          [Expand to show response]
          
14:32:10  [Ollama | qwen3:1.7b | Any]
          Q: Write a haiku
          ⏱️ 1.89s
          [Expand to show response]
```

Each row shows timestamp, backend, model, host, prompt summary, and elapsed time. Click to expand and see the full response. History is preserved during the session but cleared on page reload.

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Send Prompt | Send | Submits the prompt to the backend | `POST /api/v1/orchestrator/inference` |
| Cancel Request | X | Aborts the in-flight request; partial output is preserved | — (client-side HTTP abort) |
| Copy Response | Clipboard | Copies the full response text to clipboard | — (client-side) |
| Clear Response | Trash | Clears the response area for a fresh prompt | — (client-side) |
| Toggle Stream | Toggle | Switches between streaming and aggregated response modes | — (client-side; changes request parameter `stream=true\|false`) |
| Expand History | ChevronDown | Expands a history row to show the full prompt and response | — (client-side) |

### Walkthrough

#### Submit your first stateless inference request

1. Open the Inference view (`#/orchestration/inference`).
2. Verify that at least one agent is registered in the fleet. If not, see orchestrator-fleet.md.
3. **Backend** dropdown shows "Ollama". If it does not, select it.
4. **Host Affinity** defaults to "Any" (good for load balancing).
5. **Model** defaults to `qwen3:1.7b` (or the backend's default). You can change this if another model is installed.
6. In the **Prompt** field, enter a prompt: "Explain QUIC in three sentences."
7. Ensure **Stream** is toggled ON (default).
8. Click **Send** or press Enter.

**Expected outcome:**

```
✓ Send button becomes disabled
✓ A spinner or loading state appears
✓ Response area begins to fill with text chunks
✓ Text appears progressively as the backend streams chunks
✓ After ~2–10 seconds, the final chunk arrives with "final": true
✓ Send button re-enables
✓ Footer shows: "Model: qwen3:1.7b | Stop Reason: end_turn | Elapsed: 3.45s"
```

9. To clear the response and ask another question, click **Clear**.
10. Scroll down to **Session History** and you should see your inference call:

```
14:35:20  [Ollama | qwen3:1.7b | Any]
          Q: Explain QUIC in three sentences
          ⏱️ 3.45s
```

11. Click the history row to expand and review your prompt and response.

#### Try a session-only backend

12. Click the **Backend** dropdown.
13. Observe that "Claude", "Codex", and "OpenCode" are grayed out with tooltips:

```
This backend requires a session. Use Sessions view instead.
```

14. Attempting to send a prompt with these backends shows an error:

```
Error: 422 Unimplemented

This backend requires a persistent session. Use the Sessions view
to spawn a Claude session, then prompt it there.
```

15. To use a session-based backend, go to the Sessions view instead.

#### Handle errors

16. Leave the **Model** field as is and click **Send**.
17. If the model is not installed on the agent, you see an error after a few seconds:

```
Error: Backend returned an error

stderr: error: model 'qwen3:1.7b' not found
```

18. Fix by selecting a model that is installed, or install it on the agent.
19. If you cancel a request mid-stream by clicking **Cancel**, the partial output is preserved:

```
Response:

QUIC is a modern transport protocol that...

─────────────────────────────────────────
(request cancelled; response incomplete)

[Copy] [Clear]
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Backend dropdown is empty | No backends registered in the fleet | Register an agent via Fleet; see orchestrator-fleet.md |
| Host Affinity dropdown shows "No agents" | No agents registered for the chosen backend | Deploy an agent with the Ollama (or other) backend |
| Prompt sends but response is blank | Backend or agent timed out (>30s) | Check agent logs; consider a simpler prompt; restart the agent |
| Model field shows "not found" error | Model is not installed on the agent | Install the model: `ollama pull qwen3:1.7b` on the agent host |
| Stream toggle does not work | Browser does not support SSE or fetch streaming | Use Chrome/Firefox/Edge (Safari may have limited support) |
| Response text is gibberish or truncated | Encoding issue or network corruption | Refresh the page; try a shorter prompt; check network logs |
| Error: "Backend requires a session" | You selected Claude, Codex, or OpenCode | Use the Sessions view instead for these backends |
| Request hangs for >30 seconds | Agent is unreachable or overloaded | Check network; verify agent is running; try a different agent via Host Affinity |

### See also

- [agents/inference.md](../software-factory/user.md) — complete API and architecture reference
- [CLI: quicbaas orchestrator inference](../orchestration/cli.md#quicbaas-orchestrator-inference) — stateless inference via CLI
- Software Factory → Sessions — persistent session-based prompts (for Claude, Codex, etc.)
- Software Factory → Fleet — register and manage agents


---

## Software Factory → Jobs

### Purpose

The Jobs view displays a filterable, searchable list of orchestration jobs queued, running, or completed on the system. Use this view to monitor long-running operations, check job status, and cancel running jobs. Each job shows its ID, description, state, start time, and end time (if completed).

### Layout

The page is structured as a full-width table layout:

**Header** — "Jobs" title with "Software Factory job queue and history" subtitle. Primary CTA is **+ Start Job** button (green).

**Filters and Search** — Above the table:

- **State filter dropdown** — "All", "Active", "Completed", "Failed". Defaults to "All".
- **Search input** — real-time client-side filter on job ID and description.

**Table** — Columns:

- **Job ID** — truncated to 8 chars + ellipsis if longer (e.g., `a1b2c3d4…`)
- **Description** — user-provided job description
- **State** — badge with color (executing = blue, completed = green, failed = red, escalating = yellow)
- **Started** — timestamp in local time
- **Ended** — timestamp or "—" if still running
- **Actions** — three-dot menu with "Cancel" option (if job is cancellable)

**Empty State** — If no jobs match filters:

```
No jobs found.
Try adjusting your filters or start a new job.
[+ Start Job button]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Start Job | Plus | Opens a modal to configure and queue a new job | `POST /api/v1/orchestrator/jobs` |
| Cancel Job | Trash | Terminates a running job; shows a confirm modal | `DELETE /api/v1/orchestrator/jobs/{jobID}` |
| View Details | ChevronRight | Clicks the row to navigate to `JobDetailView` with full job logs and task breakdown | `GET /api/v1/orchestrator/jobs/{jobID}` |

### Walkthrough

#### Start and monitor your first job

1. Open the Jobs view (`#/orchestration/jobs`).
2. Note the current job count (likely 0 initially).
3. Click **+ Start Job** button.
4. A modal opens with a form:
   - **Description** — enter a descriptive name (e.g., "Email batch process")
   - **Backend** — select a registered backend (e.g., "claude" if available)
   - **Other options** — check for host affinity or priority toggles
5. Click **Start**.

**Expected outcome:**

```
✓ Modal closes
✓ Job ID appears in the table
✓ State badge shows "executing" (blue)
✓ "Started" timestamp is current
✓ "Ended" shows "—"
```

6. Leave the page open and watch the state update (jobs poll every 2–5 seconds).
7. Once the job completes, the state badge changes to "completed" (green) and "Ended" timestamp is filled.
8. Try cancelling an active job: click the row's three-dot menu and select **Cancel**.
9. A confirm modal appears:

```
Cancel Job?

Job a1b2c3d4… will be terminated. This cannot be undone.

[Keep Running] [Cancel Job]
```

10. Click **Cancel Job**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Job a1b2c3d4 cancelled"
✓ Job state changes to "cleanup" or "failed"
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Cannot start a job; modal shows "No backends available" | No backends registered in this tenant | Go to Fleet and register an agent; see orchestrator-fleet.md |
| Job stays in "executing" state indefinitely | Job is stuck or agent is unreachable | Cancel the job and check daemon logs: `quicbaas logs` |
| Cancel button is disabled/grayed out | Job is already completed or failed (not cancellable) | Only running or escalating jobs can be cancelled |
| Job ID is not clickable (no detail view) | The job detail view is not yet implemented | Use the CLI: `quicbaas orchestrator jobs status <job-id>` |

### See also

- [CLI: quicbaas orchestrator jobs](../orchestration/cli.md#orchestration-jobs) — start and list jobs via command line
- Software Factory → Workflows — parameterized multi-step job execution
- Software Factory → Live Events — real-time job event streaming


---

## Software Factory → Sessions

### Purpose

The Sessions view displays persistent ACP (Agent Conversation Protocol) sessions with stateful backends (Claude, Codex, OpenCode). Use this view to spawn new sessions, prompt active sessions, attach (replay history + tail live events), and close sessions when done. Sessions maintain conversation state across multiple prompts.

### Layout

The page displays a searchable, filterable list of sessions:

**Header** — "Sessions" title with "Active and historical sessions" subtitle. Primary CTA is **+ Spawn Session** button.

**Filters and Search** — Above the table:

- **Status filter** — "All", "Running", "Paused", "Closed". Defaults to "All".
- **Backend filter** — dropdowns to filter by backend name (e.g., "claude", "codex").
- **Tenant filter** — filter by tenant (if multi-tenant).
- **Search input** — client-side filter on session ID and description.

**Table** — Columns:

- **Session ID** — truncated UUID (e.g., `a1b2c3d4…`)
- **Description** — user-provided description or empty
- **Backend** — backend name (e.g., "claude")
- **Status** — badge: "running" (blue), "paused" (yellow), "closed" (gray)
- **Created** — ISO 8601 timestamp
- **Last Prompt** — timestamp of last prompt sent (or "—")
- **Actions** — three-dot menu with: Prompt, Attach, Close

**Empty State**:

```
No sessions.
Spawn a new ACP session to get started.

[+ Spawn Session button]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Spawn Session | Plus | Opens a modal to create a new session with a chosen backend | `POST /api/v1/orchestrator/sessions` |
| Prompt Session | MessageSquare | Opens a prompt dialog; sends prompt to the active session; updates on submit | `POST /api/v1/orchestrator/sessions/{sessionID}/prompt` |
| Attach | Eye | Navigates to `SessionDetailView` (history + live tail via SSE) | `GET /api/v1/orchestrator/sessions/{sessionID}/events` |
| Close Session | X | Terminates the session; shows a confirm modal | `DELETE /api/v1/orchestrator/sessions/{sessionID}` |

### Walkthrough

#### Spawn a session and send your first prompt

1. Open the Sessions view (`#/orchestration/sessions`).
2. Click **+ Spawn Session** button.
3. A modal form appears:

```
Spawn Session

Backend:     [Claude ▼]  (or Codex, OpenCode)
Host Affinity: [Any ▼]   (or select a specific agent)
Description: [_________________________________]
             (optional)

[Cancel] [Spawn]
```

4. Select **Claude** from the Backend dropdown (or another available backend).
5. Optionally select a **Host Affinity** to pin to a specific agent, or leave as "Any".
6. Optionally enter a **Description** (e.g., "Code review session").
7. Click **Spawn**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Session spawned"
✓ New session appears in the table with status "running" (blue)
✓ Session ID is displayed
```

8. Click your new session's row to select it.
9. In the row's three-dot menu, select **Prompt**.
10. A prompt dialog opens:

```
Send Prompt

Prompt: [_________________________________]
        [Clear] [Send Prompt]
```

11. Type a prompt (e.g., "explain QUIC in three sentences").
12. Click **Send Prompt**.

**Expected outcome:**

```
✓ Dialog closes
✓ Success toast: "Prompt sent"
✓ "Last Prompt" timestamp in the table updates to "now"
✓ (Optional) Attach to view the response in real-time
```

13. In the row's three-dot menu, select **Attach** to view the session history and live response.
14. The `SessionDetailView` opens showing:

```
Session History & Events

Session: a1b2c3d4…

[Your prompt]
You: explain QUIC in three sentences

[Live response streaming in]
Claude: QUIC is a modern transport protocol...
```

15. To close the session when done, go back to the Sessions list and click the row's three-dot menu → **Close Session**.
16. A confirm modal appears:

```
Close session a1b2c3d4…?

This session will be terminated. You cannot send more prompts to it.

[Keep Open] [Close Session]
```

17. Click **Close Session**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Session closed"
✓ Session status badge changes to "closed" (gray)
✓ Session is no longer in the "Running" filter
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Spawn Session form shows "No backends available" | No session-based backends registered (Claude, Codex, OpenCode not deployed) | Deploy an agent with a session backend; see orchestrator-fleet.md |
| Prompt returns error "Backend requires authentication" | ACP backend (Claude) needs API key or token | Set up ACP credentials in the daemon config |
| Session stays in "paused" state | Backend is slow or unreachable | Check fleet node daemon logs; restart the agent |
| Attach view shows no events | No events recorded yet (new session) | Send a prompt first, then attach |
| Close Session button is disabled | Session is already closed | Refresh the page; the session may have been closed from another client |

### See also

- [CLI: quicbaas orchestrator session](../software-factory/cli.md) — manage sessions via CLI
- [agents/inference.md](../software-factory/user.md) — session vs. sessionless inference distinction
- Software Factory → Inference — sessionless prompts (no state)
- Software Factory → Live Events — view session events in real-time


---

## Software Factory → Tasks

### Purpose

The Tasks view displays work items created by orchestration jobs. Use this view to create, claim, complete, and decompose tasks. Tasks are typically created programmatically by workflows or jobs, but the GUI allows manual creation for testing and one-off work items.

### Layout

The page lists every task in the active tenant — hand-created ones and the ones adopted from forge issues alike — and reads the generic Task model: where a task came from, how many attempts it has used, and whether it is waiting on a person.

**Header** — "Tasks" title with an "N tasks" subtitle. Controls: **All / Auto-spawned / Queued** filter, **+ New Task**, and a table/kanban layout toggle. When the list is narrowed to one project (`#/tasks?project=<id>`, reached from the **Tasks** button on a Projects row) a `Project: <name>` chip shows the filter, with an × to clear it.

**Table** — Columns:

- **Subject** — the task subject; a task adopted from a forge issue carries a `#N` chip that opens the issue on the forge
- **Project** — the project's name (its id when the project list cannot be read)
- **Status** — badge; a `blocked` task also shows a warning marker with the recorded reason, or "No reason recorded." when there is none
- **Attempts** — `charged / budget`, with `(N dispatched)` when refunded attempts make the two differ, and a muted `· N refunded` count
- **Assigned** — assignee or "—"
- **Job** — the spawned job, linking to `#/jobs/{id}`; "no agent matched" for an auto-spawn task nothing picked up
- **Updated** — relative time (exact time in the tooltip)
- **Actions** — Claim (pending), Complete (claimed / in progress), Decompose

Clicking a row opens the task's detail page. A field the server has not sent renders as "—", never as a made-up zero.

**Empty State** — "No tasks tracked — New Task is how you start." or, under a project filter, "No tasks in <project>." A failed load replaces the table with the reason: a 403 names the permission the account lacks (`orchestrator:status`).

### Task detail (`#/tasks/{id}`)

- **Header** — "Task" with the subject; **Back** and **Reload** (refetches tasks and jobs). Below it: status badge, the `#N` issue link, a `Project:` chip that opens the project's task list, and the task id.
- **Waiting on a person** — shown for `blocked` and `failed` tasks with the recorded reason ("No reason recorded." when none), the block kind, and whether the task will be retried on its own (it will not).
- **State** — status, when it last changed, priority, assignee, source (kind + ref), creator, created/updated, tenant.
- **Attempts** — charged / budget, dispatched, refunded (with the current run of refunds), and **Retry not before** ("not deferred" unless the server set a deferral).
- **Jobs** — the current spawned job (state, iteration; opens the job) and every earlier attempt's job, found through the job's `config.task_id`.
- **Pull requests** — output artifacts of kind `pull_request`, linked when they carry a URL; "No pull request recorded for this task." otherwise.
- **Issue body** — the task description in a collapsible block; **Raw Task State** — the JSON the server sent.

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| Create Task | Plus | Opens a modal to create a new task manually | `POST /api/v1/orchestrator/tasks` |
| Claim Task | Check | Claim a pending task for this agent; shows a confirm modal | `POST /api/v1/orchestrator/tasks/{taskID}/claim` |
| Complete Task | CheckDouble | Mark a claimed task as complete | `POST /api/v1/orchestrator/tasks/{taskID}/complete` |
| Decompose Task | GitBranch | Break a task into subtasks; opens a modal with JSON editor | `POST /api/v1/orchestrator/tasks/decompose` |

Deleting a task has no button in this view yet. The verb reaches every
transport and the CLI (`quicbaas orchestrator task delete <taskID>`,
`DELETE /api/v1/orchestrator/tasks/{taskID}`); it removes the task's artifacts,
claim mark and source reservation, and is refused while a job holds a live
lease on the task.

### Walkthrough

#### Create and complete your first task

1. Open the Tasks view (`#/tasks`).
2. Click **+ Create Task** button.
3. A modal form appears:

```
Create Task

Description: [_________________________________]
             (e.g., "Review PR #1234")

Metadata:    [JSON editor or key-value pairs]
             (optional, for task data)

[Cancel] [Create]
```

4. Enter a description (e.g., "Review code changes").
5. Optionally add metadata (key-value pairs or JSON).
6. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Task created"
✓ New task appears in the table with status "pending" (gray)
✓ Task ID is displayed
```

7. Find your new task in the table.
8. Click the row's three-dot menu and select **Claim Task**.
9. A confirm modal appears:

```
Claim this task?

Task will be marked as claimed and assigned to your agent.

[Cancel] [Claim Task]
```

10. Click **Claim Task**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Task claimed"
✓ Task status changes to "claimed" (blue)
✓ "Claimed By" field shows your agent name
```

11. After processing the task, click the row's three-dot menu and select **Complete Task**.
12. Another confirm modal appears:

```
Complete this task?

Task will be marked as completed. This cannot be undone.

[Cancel] [Complete Task]
```

13. Click **Complete Task**.

**Expected outcome:**

```
✓ Confirm modal closes
✓ Success toast: "Task completed"
✓ Task status changes to "completed" (green)
```

14. Optional: Try decomposing a task. Click a task's row three-dot menu and select **Decompose Task**.
15. A modal with a JSON editor opens:

```
Decompose Task

Input:   [{"subtask": "Part A", "priority": "high"}]

Output:  [{"id": "tsk_xxx", "state": "pending"}]

[Cancel] [Decompose]
```

16. Modify the JSON to define subtasks and click **Decompose**.

**Expected outcome:**

```
✓ Modal closes
✓ New subtasks appear in the task list
✓ Parent task is marked as decomposed
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Create Task form validation error | Description is empty | Enter a task description |
| Claim Task button is disabled | Task is already claimed by another agent | Complete the task first or wait for it to be released |
| Complete Task fails | Task is not in "claimed" state | Claim the task first via the Claim action |
| Decompose Task shows JSON parse error | JSON syntax is invalid | Fix the JSON and try again |
| Task does not appear after creation | API response was lost (network timeout) | Refresh the page to see the task; check daemon logs |

### See also

- [CLI: quicbaas orchestrator tasks](../software-factory/cli.md) — manage tasks via CLI
- Software Factory → Jobs — jobs that generate tasks
- Software Factory → Workflows — workflows that create tasks automatically


---

## Software Factory → Workflows

### Purpose

The Workflows view manages named, multi-step orchestration workflows. Use this view to list existing workflows, run them (with parameter entry), create new workflows (via an interactive wizard or advanced JSON editor), edit workflow definitions, duplicate for quick templating, and delete workflows. Workflows are reusable blueprints for complex orchestration logic.

### Layout

The page displays a searchable list of workflows with two main sections:

**Header** — "Workflows" title with "Multi-step orchestration templates" subtitle. Primary CTA is **+ New Workflow** button (splits into "Wizard" and "Advanced" sub-buttons).

**Search input** — client-side filter on workflow name and description.

**Cards or Table** — Each workflow shows:

- **Name** — workflow identifier
- **Description** — short summary (if provided)
- **Created** — ISO 8601 timestamp
- **Last Run** — timestamp or "Never"
- **Actions** — three-dot menu with: Run, Edit, Duplicate, Delete

**Empty State**:

```
No workflows.
Create your first workflow using the wizard or advanced JSON editor.

[+ Wizard] [+ Advanced]
```

### Lifecycle actions

| Action | Icon | What it does | Backend endpoint |
|--------|------|-------------|-----------------|
| New Workflow (Wizard) | Plus | Opens a step-by-step form to define a workflow interactively | `POST /api/v1/orchestrator/workflows` |
| New Workflow (Advanced) | Plus | Opens a raw JSON editor for power users | `POST /api/v1/orchestrator/workflows` |
| Run Workflow | Play | Opens a parameter-entry form; executes the workflow as a job | `POST /api/v1/orchestrator/jobs` (with workflow ref) |
| Edit Workflow | Edit | Opens the workflow JSON editor for inline modifications | `PUT /api/v1/orchestrator/workflows/{name}` |
| Duplicate Workflow | Copy | Creates a copy of the workflow with a new name | `POST /api/v1/orchestrator/workflows` (with copied JSON) |
| Delete Workflow | Trash | Removes the workflow definition | `DELETE /api/v1/orchestrator/workflows/{name}` |

### Walkthrough

#### Create a workflow using the wizard

1. Open the Workflows view (`#/orchestration/workflows`).
2. Click **+ New Workflow** button and select **Wizard**.
3. A multi-step modal dialog opens:

**Step 1 — Basic Info**

```
Workflow Name:  [email-campaign-v1________]

Description:    [Send emails and track responses________]
                (optional)

[Next] [Cancel]
```

4. Enter a **Workflow Name** (no spaces; use hyphens or underscores).
5. Optionally enter a **Description**.
6. Click **Next**.

**Step 2 — Add Steps**

```
Steps:

[+ Add Step]

Step 1:  Type: [Prompt ▼]       Backend: [Claude ▼]
         Prompt: [user_email content__________]
         [Edit] [Delete]

[+ Add Step]

[Back] [Next] [Cancel]
```

7. Click **+ Add Step** to add the first step.
8. A sub-form appears:

```
Step Type:   [Prompt ▼]  (or Task, Inference, Wait, etc.)
Backend:     [Claude ▼]  (if Prompt selected)
Prompt:      [Who is the recipient?_____]
Host Affinity: [Any ▼]
[Save Step] [Cancel]
```

9. Fill in the step details. For a Prompt step, select Claude, enter a prompt.
10. Click **Save Step**.

**Expected outcome:**

```
✓ Step 1 appears in the Steps list
✓ Step summary shows: "Prompt to Claude: Who is the recipient?"
```

11. Click **+ Add Step** to add a second step (optional).
12. Add another step (e.g., "Wait 10 seconds" or "Check response").
13. When done adding steps, click **Next**.

**Step 3 — Outputs**

```
Workflow Output:

Result fields to capture:

[+ Add Output]

Output 1: [response_text________] Type: [String ▼]
          [Edit] [Delete]

[Back] [Create] [Cancel]
```

14. Optionally define output fields to capture results from steps.
15. Click **Create**.

**Expected outcome:**

```
✓ Modal closes
✓ Success toast: "Workflow created"
✓ Workflow appears in the list
```

#### Run the workflow

16. Click your new workflow's row.
17. Click the three-dot menu and select **Run Workflow**.
18. A parameter-entry dialog opens:

```
Run Workflow: email-campaign-v1

Parameters:

recipient_email: [user@example.com_________]
            
[Run] [Cancel]
```

19. Fill in any required parameters.
20. Click **Run**.

**Expected outcome:**

```
✓ Dialog closes
✓ Success toast: "Workflow job started"
✓ A new job appears in the Jobs view
```

#### Edit and duplicate workflows

21. Click a workflow's three-dot menu and select **Edit**.
22. A JSON editor opens with the full workflow definition:

```json
{
  "name": "email-campaign-v1",
  "description": "Send emails...",
  "steps": [
    {
      "type": "prompt",
      "backend": "claude",
      "prompt": "Who is the recipient?"
    }
  ]
}
```

23. Modify the JSON as needed and click **Save**.
24. Alternatively, click **Duplicate** to create a copy with a new name (e.g., `email-campaign-v2`).

**Expected outcome:**

```
✓ Workflow is saved with changes
✓ Toast confirms: "Workflow updated"
✓ Duplicated workflow appears in the list
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| Workflow name already exists | Another workflow has the same name | Choose a unique name; append `-v2`, `-prod`, etc. |
| Wizard form validation fails | Required fields are empty (name, at least one step) | Fill in all required fields before clicking Next |
| Run Workflow shows "Missing parameters" | Workflow expects input that you didn't provide | Check the workflow definition; provide all required parameters |
| JSON editor shows parse error | JSON syntax is invalid | Fix syntax errors (missing quotes, commas, braces) |
| Workflow step uses nonexistent backend | Backend is not registered in the fleet | Register the backend via Fleet; edit the workflow to use an available backend |
| Delete Workflow is disabled | Workflow is currently running | Wait for the running job to complete or cancel it from the Jobs view |

### See also

- [CLI: quicbaas orchestrator workflows](../orchestration/cli.md#workflow-management) — manage workflows via CLI
- Software Factory → Jobs — monitor workflow execution as jobs
- Software Factory → Inference — single-step stateless prompts
- [configuration/workflow-settings.md](../software-factory/engineering.md) — advanced workflow configuration

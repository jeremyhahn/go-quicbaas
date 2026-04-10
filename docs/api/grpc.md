# gRPC API

High-performance binary protocol for database operations.

## Protocol Definition

```
api/proto/quicbaas.proto
```

## Service Overview

```protobuf
service QuicBaaS {
  // CRUD Operations
  rpc Put(PutRequest) returns (PutResponse);
  rpc Get(GetRequest) returns (GetResponse);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
  rpc List(ListRequest) returns (stream ListResponse);

  // Index Operations
  rpc Query(QueryRequest) returns (stream QueryResponse);
  rpc ScanIndex(ScanRequest) returns (stream ScanResponse);

  // Cluster Management
  rpc ClusterStatus(ClusterStatusRequest) returns (ClusterStatusResponse);
  rpc GetLeader(GetLeaderRequest) returns (GetLeaderResponse);

  // Time-Series Operations
  rpc Aggregate(AggregateRequest) returns (AggregateResponse);
  rpc TimeSeriesQuery(TimeSeriesQueryRequest) returns (stream TimeSeriesQueryResponse);

  // Batch Operations
  rpc BatchPut(BatchPutRequest) returns (BatchPutResponse);
  rpc BatchGet(BatchGetRequest) returns (BatchGetResponse);
  rpc BatchDelete(BatchDeleteRequest) returns (BatchDeleteResponse);

  // Transaction Operations
  rpc Transaction(TransactionRequest) returns (TransactionResponse);

  // Health and Diagnostics
  rpc Health(HealthRequest) returns (HealthResponse);
}
```

## Server Connection

### Unix Socket (Default)

Unix domain sockets are the default and recommended transport for local communication:

```
unix:///tmp/quicbaas.sock          (development)
unix:///var/run/quicbaas/quicbaas.sock  (production)
```

Unix sockets provide:
- Lower latency than TCP (no network stack overhead)
- No port management required
- Enhanced security through filesystem permissions
- Automatic detection from bare paths ending in `.sock`
- **Peer credentials authentication** via SO_PEERCRED (kernel-verified UID/GID/PID)

#### Peer Credentials Authentication

QuicBaaS extracts peer credentials from Unix socket connections using the Linux `SO_PEERCRED` socket option. These credentials are kernel-verified and cannot be spoofed by the connecting process.

**Types:**

| Type | Description |
|------|-------------|
| `PeerCredentials` | Contains PID, UID, and GID of the connecting process |
| `PeerCredentialsAuthInfo` | Implements gRPC `credentials.AuthInfo` |
| `UnixTransportCredentials` | Implements gRPC `credentials.TransportCredentials` |

**Verifiers:**

| Verifier | Description |
|----------|-------------|
| `AllowAllVerifier` | Allows all credentials (testing/open environments) |
| `RootOnlyVerifier` | Only allows root (UID 0) |
| `UIDVerifier` | Allows specific UIDs (with root bypass) |
| `GIDVerifier` | Allows specific GIDs (with root bypass) |
| `ChainVerifier` | Chains multiple verifiers (all must pass) |

**Server-side usage:**

```go
import grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"

// Create transport credentials
creds := grpcapi.NewUnixTransportCredentials()

// Create a UID-based verifier (allow UIDs 1000 and 1001)
verifier := grpcapi.NewUIDVerifier(1000, 1001)

// Create interceptors
unaryInterceptor := grpcapi.UnaryPeerCredentialsInterceptor(verifier, logger)
streamInterceptor := grpcapi.StreamPeerCredentialsInterceptor(verifier, logger)

// Create gRPC server with credentials
server := grpc.NewServer(
    grpc.Creds(creds),
    grpc.UnaryInterceptor(unaryInterceptor),
    grpc.StreamInterceptor(streamInterceptor),
)
```

**Extracting credentials in handlers:**

```go
func (s *Server) SomeMethod(ctx context.Context, req *pb.Request) (*pb.Response, error) {
    creds := grpcapi.PeerCredentialsFromContext(ctx)
    if creds != nil {
        log.Printf("Request from PID %d, UID %d, GID %d", creds.PID, creds.UID, creds.GID)
        if creds.IsRoot() {
            // Handle root user
        }
    }
    // ...
}
```

**Platform support:**
- **Linux**: Full SO_PEERCRED support
- **macOS/Windows**: Stub implementation (returns error for credential extraction)

### TCP

TCP listeners are optional and must be explicitly enabled:

```
localhost:50051    (insecure)
localhost:50052    (TLS)
```

Enable TCP with `--enable-tcp` flag or `enable_tcp: true` in configuration.

## Authentication

### Unix Socket Connection (Recommended)

```go
// Using bare socket path (auto-detected)
conn, err := grpc.Dial("unix:///tmp/quicbaas.sock", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

The client automatically detects unix sockets from:
- `unix://` prefix: `unix:///tmp/quicbaas.sock`
- `unix-abstract://` prefix: `unix-abstract://quicbaas`
- Bare paths ending in `.sock`: `/tmp/quicbaas.sock`

### Insecure TCP Connection (Development)

```go
conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

### TLS Connection (Production)

```go
creds, err := credentials.NewClientTLSFromFile("ca.crt", "")
if err != nil {
    log.Fatal(err)
}

conn, err := grpc.Dial("localhost:50052",
    grpc.WithTransportCredentials(creds),
)
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

client := proto.NewQuicBaaSClient(conn)
```

### Mutual TLS (mTLS)

```go
cert, err := tls.LoadX509KeyPair("client.crt", "client.key")
if err != nil {
    log.Fatal(err)
}

caCert, err := os.ReadFile("ca.crt")
if err != nil {
    log.Fatal(err)
}

caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
}

creds := credentials.NewTLS(tlsConfig)
conn, err := grpc.Dial("localhost:50052",
    grpc.WithTransportCredentials(creds),
)
```

## API Reference

### Put Operation

```go
req := &proto.PutRequest{
    EntityType:  "user",
    Id:          1,
    Data:        []byte(`{"name":"John","email":"john@example.com"}`),
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    Metadata: map[string]string{
        "source": "api",
        "version": "1.0",
    },
}

resp, err := client.Put(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Stored with ID: %d, Version: %d\n", resp.Id, resp.Version)
```

**Request Fields:**
- `entity_type` (string) - Entity category (e.g., "user", "sensor")
- `id` (uint64) - Unique identifier (0 for auto-generated)
- `data` (bytes) - Serialized entity data
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `ttl_seconds` (uint64) - Time-to-live (0 = no expiration)
- `metadata` (map<string,string>) - Optional key-value metadata
- `shard_id` (uint64) - Target shard (0 = auto-assign)

**Response Fields:**
- `id` (uint64) - Assigned or provided identifier
- `version` (uint64) - Entity version number
- `timestamp` (int64) - Server timestamp (Unix nanoseconds)
- `leader_id` (uint64) - Node that handled the write

### Get Operation

```go
req := &proto.GetRequest{
    EntityType:      "user",
    Id:              1,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: true,
}

resp, err := client.Get(ctx, req)
if err != nil {
    log.Fatal(err)
}

if !resp.Found {
    log.Println("Entity not found")
    return
}

fmt.Printf("Data: %s\n", string(resp.Data))
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `id` (uint64) - Unique identifier
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = auto-detect)
- `include_metadata` (bool) - Return metadata with response

**Response Fields:**
- `id` (uint64) - Unique identifier
- `data` (bytes) - Serialized entity data
- `version` (uint64) - Current entity version
- `timestamp` (int64) - Last modified timestamp (Unix nanoseconds)
- `metadata` (map<string,string>) - Optional metadata
- `found` (bool) - Whether the key exists

### Delete Operation

```go
req := &proto.DeleteRequest{
    EntityType:  "user",
    Id:          1,
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    Version:     1, // Optional: for optimistic locking
}

resp, err := client.Delete(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Deleted: %v\n", resp.Deleted)
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `id` (uint64) - Unique identifier to delete
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = auto-detect)
- `version` (uint64) - Optional version for optimistic locking (0 = no check)

**Response Fields:**
- `deleted` (bool) - Whether the key existed and was deleted
- `timestamp` (int64) - Deletion timestamp (Unix nanoseconds)

### List Operation

```go
req := &proto.ListRequest{
    EntityType:      "user",
    PageSize:        100,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: false,
}

stream, err := client.List(ctx, req)
if err != nil {
    log.Fatal(err)
}

for {
    resp, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }

    for _, entity := range resp.Entities {
        fmt.Printf("ID: %d, Data: %s\n", entity.Id, string(entity.Data))
    }

    if resp.NextPageToken == "" {
        break
    }
}
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `page_size` (uint32) - Results per page
- `page_token` (string) - Continuation token from previous response
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `shard_id` (uint64) - Target shard (0 = all shards)
- `include_metadata` (bool) - Return metadata with entities

**Response Fields:**
- `entities` ([]Entity) - Batch of results
- `next_page_token` (string) - Token for next page (empty if last)
- `total_count` (uint64) - Total number of entities (if available)

### ListEntityTypes Operation

Discover all unique entity types stored in the database. In multi-tenant mode, only returns entity types within the tenant's keyspace.

```go
req := &proto.ListEntityTypesRequest{
    PageSize:  100,
    PageToken: "",
}

resp, err := client.ListEntityTypes(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, entityType := range resp.EntityTypes {
    fmt.Printf("Entity Type: %s\n", entityType)
}

if resp.NextPageToken != "" {
    // Fetch more results with resp.NextPageToken
}
```

**Request Fields:**
- `page_size` (uint32) - Maximum results per page (default: 100, max: 1000)
- `page_token` (string) - Continuation token from previous response
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `timeout_ms` (int64) - Operation timeout in milliseconds

**Response Fields:**
- `entity_types` ([]string) - List of unique entity type names
- `next_page_token` (string) - Token for next page (empty if last)
- `total_count` (uint64) - Total number of entity types (if available)

### Query Operation

```go
req := &proto.QueryRequest{
    EntityType:      "user",
    IndexName:       "email",
    Value:           []byte("john@example.com"),
    PageSize:        50,
    Consistency:     proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    IncludeMetadata: false,
}

stream, err := client.Query(ctx, req)
if err != nil {
    log.Fatal(err)
}

for {
    resp, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }

    for _, entity := range resp.Entities {
        fmt.Printf("Found: ID=%d, Data=%s\n", entity.Id, string(entity.Data))
    }
}
```

**Request Fields:**
- `entity_type` (string) - Entity category
- `index_name` (string) - Name of the index to query
- `value` (bytes) - Exact value to match (for equality queries)
- `range_start` (bytes) - Inclusive start for range queries
- `range_end` (bytes) - Inclusive end for range queries
- `page_size` (uint32) - Number of results
- `page_token` (string) - Continuation token
- `consistency` (ConsistencyLevel) - LOCAL or QUORUM
- `include_metadata` (bool) - Return metadata

**Response Fields:**
- `entities` ([]Entity) - Matching entities
- `next_page_token` (string) - Pagination token
- `total_count` (uint64) - Total matches (if available)

### Cluster Status

```go
req := &proto.ClusterStatusRequest{
    IncludeMetrics: true,
}

resp, err := client.ClusterStatus(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Cluster ID: %s\n", resp.ClusterId)
for _, node := range resp.Nodes {
    fmt.Printf("Node %d: %s (Leader: %v, Health: %s)\n",
        node.NodeId, node.Address, node.IsLeader, node.HealthStatus)
}
```

**Response Fields:**
- `cluster_id` (string) - Unique cluster identifier
- `nodes` ([]NodeInfo) - Information about all nodes
- `shards` ([]ShardInfo) - Information about all shards
- `timestamp` (int64) - Status collection time (Unix nanoseconds)

### Batch Put

```go
req := &proto.BatchPutRequest{
    Requests: []*proto.PutRequest{
        {
            EntityType: "user",
            Id:         1,
            Data:       []byte(`{"name":"John"}`),
        },
        {
            EntityType: "user",
            Id:         2,
            Data:       []byte(`{"name":"Jane"}`),
        },
    },
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
}

resp, err := client.BatchPut(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, r := range resp.Responses {
    fmt.Printf("Stored ID %d, Version %d\n", r.Id, r.Version)
}
```

### Health Check

```go
req := &proto.HealthRequest{
    IncludeDetails: true,
}

resp, err := client.Health(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Status: %s, Uptime: %d seconds\n",
    resp.Status, resp.UptimeSeconds)
```

### Transaction

Atomic compare-and-swap operations with ACID guarantees.

```go
// Example: Optimistic locking update
req := &proto.TransactionRequest{
    // Compare conditions: all must succeed
    Compare: []*proto.CompareCondition{
        {
            EntityType: "user",
            Id:         1,
            Target:     proto.CompareTarget_COMPARE_TARGET_VERSION,
            Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
            Version:    5, // Expected current version
        },
    },
    // Success operations: execute if all comparisons succeed
    Success: []*proto.TransactionOp{
        {
            OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
            EntityType: "user",
            Id:         1,
            Data:       []byte(`{"name":"John Updated","email":"john@example.com"}`),
        },
    },
    // Failure operations: execute if any comparison fails
    Failure: []*proto.TransactionOp{
        {
            OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_GET,
            EntityType: "user",
            Id:         1,
        },
    },
    Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
}

resp, err := client.Transaction(ctx, req)
if err != nil {
    log.Fatal(err)
}

if resp.Succeeded {
    fmt.Println("Update successful")
} else {
    fmt.Println("Version conflict detected")
    // Check failure operations results
    for _, opResp := range resp.Responses {
        if opResp.OpType == proto.TransactionOpType_TRANSACTION_OP_TYPE_GET {
            fmt.Printf("Current version: %d\n", opResp.Version)
        }
    }
}
```

**Request Fields:**
- `compare` ([]CompareCondition) - Predicates to evaluate (all must succeed)
- `success` ([]TransactionOp) - Operations to execute if all comparisons succeed
- `failure` ([]TransactionOp) - Operations to execute if any comparison fails
- `consistency` (ConsistencyLevel) - Transaction consistency level
- `shard_id` (uint64) - Target shard (must be same for all operations)
- `timeout_ms` (int64) - Maximum time to wait (default: 30000ms)

**Compare Targets:**
- `VERSION` - Compare entity version number
- `VALUE` - Compare entity data value
- `EXISTS` - Check if entity exists
- `CREATE` - Check entity creation timestamp
- `MOD` - Check entity modification timestamp

**Compare Operators:**
- `EQUAL` (==)
- `NOT_EQUAL` (!=)
- `GREATER` (>)
- `LESS` (<)
- `GREATER_EQUAL` (>=)
- `LESS_EQUAL` (<=)

**Transaction Operations:**
- `PUT` - Create or update entity
- `GET` - Retrieve entity
- `DELETE` - Delete entity

**Response Fields:**
- `succeeded` (bool) - Whether all comparisons succeeded
- `responses` ([]TransactionOpResponse) - Results of executed operations
- `timestamp` (int64) - Transaction completion time
- `message` (string) - Additional information
- `revision` (uint64) - MVCC revision

**Common Patterns:**

Conditional Put (only if exists):
```go
Compare: []*proto.CompareCondition{
    {
        EntityType: "counter",
        Id:         1,
        Target:     proto.CompareTarget_COMPARE_TARGET_EXISTS,
        Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
        Version:    1, // 1 = exists, 0 = doesn't exist
    },
}
```

Conditional Delete (only if value matches):
```go
Compare: []*proto.CompareCondition{
    {
        EntityType: "session",
        Id:         123,
        Target:     proto.CompareTarget_COMPARE_TARGET_VALUE,
        Operator:   proto.CompareOperator_COMPARE_OPERATOR_EQUAL,
        Value:      []byte(`{"status":"expired"}`),
    },
}
```

Multi-operation transaction:
```go
Success: []*proto.TransactionOp{
    {
        OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
        EntityType: "account",
        Id:         1,
        Data:       []byte(`{"balance":900}`),
    },
    {
        OpType:     proto.TransactionOpType_TRANSACTION_OP_TYPE_PUT,
        EntityType: "account",
        Id:         2,
        Data:       []byte(`{"balance":1100}`),
    },
}
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "time"

    "google.golang.org/grpc"
    proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func main() {
    // Connect via Unix socket (recommended for local connections)
    conn, err := grpc.Dial("unix:///tmp/quicbaas.sock", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()

    client := proto.NewQuicBaaSClient(conn)
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    // Put operation
    putReq := &proto.PutRequest{
        EntityType:  "user",
        Id:          1,
        Data:        []byte(`{"name":"John","email":"john@example.com"}`),
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    }

    putResp, err := client.Put(ctx, putReq)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Put: ID=%d, Version=%d", putResp.Id, putResp.Version)

    // Get operation
    getReq := &proto.GetRequest{
        EntityType:  "user",
        Id:          1,
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_LOCAL,
    }

    getResp, err := client.Get(ctx, getReq)
    if err != nil {
        log.Fatal(err)
    }

    if getResp.Found {
        log.Printf("Get: %s", string(getResp.Data))
    }

    // Delete operation
    deleteReq := &proto.DeleteRequest{
        EntityType:  "user",
        Id:          1,
        Consistency: proto.ConsistencyLevel_CONSISTENCY_LEVEL_QUORUM,
    }

    deleteResp, err := client.Delete(ctx, deleteReq)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Delete: %v", deleteResp.Deleted)
}
```

## Error Handling

gRPC errors use standard status codes:

```go
import (
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

resp, err := client.Get(ctx, req)
if err != nil {
    st, ok := status.FromError(err)
    if !ok {
        log.Fatal("unknown error")
    }

    switch st.Code() {
    case codes.NotFound:
        log.Println("Entity not found")
    case codes.Unavailable:
        log.Println("Service unavailable")
    case codes.DeadlineExceeded:
        log.Println("Request timeout")
    default:
        log.Printf("Error: %v", st.Message())
    }
}
```

Common error codes:

| Code | Description |
|------|-------------|
| `NOT_FOUND` | Entity not found |
| `INVALID_ARGUMENT` | Invalid request parameters |
| `UNAVAILABLE` | Service temporarily unavailable |
| `DEADLINE_EXCEEDED` | Request timeout |
| `INTERNAL` | Internal server error |
| `UNAUTHENTICATED` | Authentication required |
| `PERMISSION_DENIED` | Permission denied |

## Performance

### Connection Pooling

```go
conn, err := grpc.Dial("localhost:50051",
    grpc.WithInsecure(),
    grpc.WithDefaultCallOptions(
        grpc.MaxCallRecvMsgSize(10*1024*1024),
        grpc.MaxCallSendMsgSize(10*1024*1024),
    ),
)
```

### Compression

```go
import "google.golang.org/grpc/encoding/gzip"

resp, err := client.Put(ctx, req,
    grpc.UseCompressor(gzip.Name),
)
```

### Streaming

```go
stream, err := client.List(ctx, &proto.ListRequest{
    EntityType: "user",
    PageSize:   1000,
})
```

### Keepalive

```go
conn, err := grpc.Dial("localhost:50051",
    grpc.WithInsecure(),
    grpc.WithKeepaliveParams(keepalive.ClientParameters{
        Time:                10 * time.Second,
        Timeout:             3 * time.Second,
        PermitWithoutStream: true,
    }),
)
```

## Ollama Service

gRPC service for Ollama embedding generation and model management.

### Protocol Definition

```
api/proto/ollama.proto
```

### Service Overview

```protobuf
service OllamaService {
  // Health returns the health status of the Ollama connection.
  rpc Health(OllamaHealthRequest) returns (OllamaHealthResponse);

  // ListModels returns the list of available Ollama models.
  rpc ListModels(OllamaListModelsRequest) returns (OllamaListModelsResponse);

  // GetModelInfo returns detailed information about a specific model.
  rpc GetModelInfo(OllamaGetModelInfoRequest) returns (OllamaGetModelInfoResponse);

  // Embed generates an embedding for a single text using the default model.
  rpc Embed(OllamaEmbedRequest) returns (OllamaEmbedResponse);

  // EmbedWithModel generates an embedding for a single text using a specific model.
  rpc EmbedWithModel(OllamaEmbedWithModelRequest) returns (OllamaEmbedResponse);

  // EmbedBatch generates embeddings for multiple texts.
  rpc EmbedBatch(OllamaEmbedBatchRequest) returns (OllamaEmbedBatchResponse);
}
```

### Health Check

```go
req := &proto.OllamaHealthRequest{
    FullCheck: true,
}

resp, err := client.Health(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Available: %v, Version: %s\n", resp.Available, resp.Version)
```

### List Models

```go
req := &proto.OllamaListModelsRequest{
    NamePrefix: "nomic", // Optional filter
}

resp, err := client.ListModels(ctx, req)
if err != nil {
    log.Fatal(err)
}

for _, model := range resp.Models {
    fmt.Printf("Model: %s, Size: %d bytes\n", model.Name, model.Size)
}
```

### Generate Embedding

```go
req := &proto.OllamaEmbedRequest{
    Text: "What is QuicBaaS?",
}

resp, err := client.Embed(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Model: %s, Dimensions: %d\n", resp.Model, resp.Dimensions)
fmt.Printf("Embedding: %v\n", resp.Embedding[:5]) // First 5 values
```

### Batch Embedding

```go
req := &proto.OllamaEmbedBatchRequest{
    Texts: []string{
        "First document",
        "Second document",
        "Third document",
    },
    Model: "nomic-embed-text", // Optional
}

resp, err := client.EmbedBatch(ctx, req)
if err != nil {
    log.Fatal(err)
}

for i, emb := range resp.Embeddings {
    fmt.Printf("Document %d: %d dimensions\n", i, len(emb.Values))
}
```

## RAG Service

gRPC service for RAG context building operations.

### Protocol Definition

```
api/proto/rag.proto
```

### Service Overview

```protobuf
service RAGService {
  // Query performs an all-in-one RAG query: embed, search, and build context.
  rpc Query(RAGQueryRequest) returns (RAGQueryResponse);

  // BuildContext assembles context from user-provided search results.
  rpc BuildContext(RAGBuildContextRequest) returns (RAGBuildContextResponse);
}
```

### RAG Query

```go
req := &proto.RAGQueryRequest{
    Query:           "How does error handling work?",
    Namespace:       "codebase",
    TopK:            10,
    MaxTokens:       4000,
    MinScore:        0.7,
    Template:        proto.RAG_TEMPLATE_MARKDOWN,
    IncludeCitations: true,
}

resp, err := client.Query(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Context (%d tokens):\n%s\n", resp.Tokens, resp.Context)
fmt.Printf("Results: %d, Truncated: %v\n", resp.ResultCount, resp.Truncated)

for _, citation := range resp.Citations {
    fmt.Printf("[%d] %s:%d-%d (score: %.2f)\n",
        citation.Index, citation.FilePath,
        citation.StartLine, citation.EndLine, citation.Score)
}
```

### Build Context

```go
req := &proto.RAGBuildContextRequest{
    Results: []*proto.RAGResultInput{
        {
            Content: "func main() {\n    fmt.Println(\"Hello\")\n}",
            Score:   0.92,
            Metadata: map[string]string{
                "file_path":  "main.go",
                "start_line": "1",
                "end_line":   "10",
            },
        },
        {
            Content: "type Config struct {\n    Name string\n}",
            Score:   0.85,
            Metadata: map[string]string{
                "file_path":  "config.go",
                "start_line": "5",
                "end_line":   "8",
            },
        },
    },
    MaxTokens:          2000,
    Template:           proto.RAG_TEMPLATE_MARKDOWN,
    IncludeCitations:   true,
    TruncationStrategy: proto.RAG_TRUNCATION_SMART,
}

resp, err := client.BuildContext(ctx, req)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Context:\n%s\n", resp.Context)
fmt.Printf("Tokens: %d, Results: %d\n", resp.Tokens, resp.ResultCount)
```

### Template Options

| Template | Description |
|----------|-------------|
| `RAG_TEMPLATE_MARKDOWN` | Markdown-formatted context with code blocks |
| `RAG_TEMPLATE_JSON` | JSON-structured context |
| `RAG_TEMPLATE_XML` | XML-formatted context |
| `RAG_TEMPLATE_PLAIN` | Plain text without formatting |

### Truncation Strategies

| Strategy | Description |
|----------|-------------|
| `RAG_TRUNCATION_SMART` | Preserve complete chunks, drop lowest-scored |
| `RAG_TRUNCATION_HARD` | Hard cut at token limit |
| `RAG_TRUNCATION_NONE` | No truncation, may exceed limit |

## See Also

- [CLI Documentation](../cli/README.md) - Command-line interface
- [REST API](../rest/README.md) - HTTP/JSON interface
- [Architecture](../architecture/README.md) - System design
- [RAG MCP Tools](../rag/mcp-tools.md) - RAG MCP tool reference

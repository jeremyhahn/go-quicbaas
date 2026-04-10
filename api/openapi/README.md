# QuicBaaS OpenAPI Documentation

This directory contains the OpenAPI 3.0 specification for the QuicBaaS REST API.

## Files

- `quicbaas-v1.yaml` - Complete OpenAPI 3.0 specification for QuicBaaS REST API v1

## Viewing the Documentation

### Option 1: Swagger UI (Recommended)

When running a QuicBaaS server, navigate to:

```
http://localhost:8080/docs/
```

This will display an interactive Swagger UI where you can:
- Browse all available endpoints
- View request/response schemas
- Try out API calls directly from the browser
- Download the OpenAPI specification

### Option 2: Swagger Editor

Open [Swagger Editor](https://editor.swagger.io/) and import `quicbaas-v1.yaml` to view and edit the specification.

### Option 3: Command Line

View the raw spec:
```bash
curl http://localhost:8080/docs/openapi.yaml
```

Or use a tool like `swagger-cli`:
```bash
npm install -g @apidevtools/swagger-cli
swagger-cli validate quicbaas-v1.yaml
```

## API Overview

### Base URL

```
http://localhost:8080/v1
```

### Available Endpoints

#### Entities (CRUD Operations)
- `POST /v1/entities/{type}` - Create a new entity
- `GET /v1/entities/{type}` - List entities with pagination
- `GET /v1/entities/{type}/{id}` - Get entity by ID
- `PUT /v1/entities/{type}/{id}` - Update entity
- `DELETE /v1/entities/{type}/{id}` - Delete entity

#### Queries (Index-based Search)
- `GET /v1/query/{type}/{index}?value=...` - Query by exact value
- `POST /v1/query/range` - Range query on indexed fields

#### Cluster Management
- `GET /v1/cluster/status` - Get cluster status and node information
- `GET /v1/cluster/leader/{shardID}` - Get leader for a specific shard

#### Health & Monitoring
- `GET /v1/health` - Overall health check
- `GET /v1/health/live` - Liveness probe (Kubernetes compatible)
- `GET /v1/health/ready` - Readiness probe (Kubernetes compatible)
- `GET /v1/metrics` - Prometheus metrics endpoint

## Examples

### Create an Entity

```bash
curl -X POST http://localhost:8080/v1/entities/user \
  -H "Content-Type: application/json" \
  -d '{
    "data": {
      "name": "John Doe",
      "email": "john@example.com",
      "age": 30
    },
    "metadata": {
      "source": "api",
      "version": "1.0"
    }
  }'
```

### Get an Entity

```bash
curl http://localhost:8080/v1/entities/user/12345
```

### Query by Index

```bash
curl 'http://localhost:8080/v1/query/user/email_idx?value=john@example.com'
```

### Check Cluster Status

```bash
curl http://localhost:8080/v1/cluster/status
```

## Consistency Levels

QuicBaaS supports two consistency levels:

- **local** (default) - Reads/writes are performed on the local node only. Fastest, but may return stale data during network partitions.
- **quorum** - Requires majority acknowledgment. Ensures linearizability at the cost of latency.

Specify via query parameter:
```bash
curl 'http://localhost:8080/v1/entities/user/123?consistency=quorum'
```

## Response Format

All responses are in JSON format.

### Success Response

```json
{
  "id": 12345,
  "data": { ... },
  "version": 1,
  "timestamp": "2025-10-05T10:30:00Z"
}
```

### Error Response

```json
{
  "error": "INVALID_INPUT",
  "message": "Invalid entity type",
  "request_id": "req-abc123",
  "timestamp": "2025-10-05T10:30:00Z"
}
```

## Updating the Specification

When adding new endpoints or modifying existing ones:

1. Update `quicbaas-v1.yaml` with the changes
2. Validate the spec:
   ```bash
   swagger-cli validate quicbaas-v1.yaml
   ```
3. Test the changes in Swagger UI
4. Commit the updated specification

## Tools & Resources

- [OpenAPI Specification](https://spec.openapis.org/oas/v3.0.3)
- [Swagger Editor](https://editor.swagger.io/)
- [Swagger UI](https://swagger.io/tools/swagger-ui/)
- [Swagger CLI](https://github.com/APIDevTools/swagger-cli)

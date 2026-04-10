# Changelog

All notable changes to QuicBaaS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-02-03

### First Public Release

QuicBaaS is a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus and PebbleDB storage. Designed for speed, strong consistency, horizontal scalability, security, and operational simplicity.

### Core Features

#### Storage & Data
- **Multi-Raft Consensus** - Distributed consensus using QuicRaft with configurable write modes (sync, WAL, batched)
- **PebbleDB Storage Engine** - High-performance LSM-tree storage with MVCC support
- **Key-Value Operations** - Full CRUD with transactions, batch operations, and prefix queries
- **Vector Database** - HNSW and IVF indexing for similarity search
- **Time-Series Database** - Metrics storage with retention policies and downsampling
- **Object Storage** - Multi-backend support (S3, GCS, Azure, MinIO) with tiering

#### Security
- **IAM/ABAC** - Identity and access management with attribute-based access control
- **KMS Integration** - Support for AWS KMS, GCP KMS, Azure Key Vault, HashiCorp Vault, PKCS#11
- **Secrets Engine** - Versioned secrets with rotation and watch API
- **Encryption** - At-rest and in-transit encryption with FIPS 140-3 compliance option
- **Seal/Unseal** - Vault-style initialization with Shamir secret sharing
- **mTLS** - Mutual TLS for all inter-node communication

#### APIs & Protocols
- **gRPC** - High-performance RPC with streaming support
- **REST/HTTP** - RESTful API with SSE for real-time updates
- **QUIC** - Low-latency UDP-based transport
- **WebSocket** - Real-time communication with MQTT, AMQP, STOMP subprotocols
- **MCP** - Model Context Protocol for AI/LLM integration (270+ tools)
- **Unix Socket** - Local IPC for embedded use cases

#### AI & Machine Learning
- **RAG Pipeline** - Retrieval-Augmented Generation with chunking, embedding, and ranking
- **Embedding Providers** - Support for OpenAI, Ollama, and custom providers
- **Vector Search** - Semantic similarity search with configurable distance metrics

#### Messaging & Real-Time
- **Push Notifications** - Channel-based pub/sub with delta publishing
- **Watch System** - Real-time change notifications with delta computation
- **Message Deduplication** - Content-based and ID-based deduplication

#### Operations
- **Distributed Scheduler** - Cron-based job scheduling across the cluster
- **Backup & Recovery** - Point-in-time recovery with multiple storage backends
- **Cache Layer** - L1 in-memory cache with optional L2 Redis
- **Cluster Management** - Dynamic node addition/removal, rebalancing

#### SDKs
- **Go SDK** - Full-featured client with all transports
- **JavaScript SDK** - Browser and Node.js support with 100% test coverage

#### CLI
- **308 Commands** - Comprehensive CLI across 24 categories
- **Multiple Transports** - gRPC, REST, QUIC, Unix socket, embedded

### Build Profiles
- **Minimal** - Core KV + gRPC only
- **Embedded** - Single-node, no clustering
- **Standard** - Cluster mode, no AI features
- **Cluster** - All features, no cloud KMS
- **Enterprise** - All features + cloud KMS
- **Full** - Everything including HSM and quantum-safe crypto

### Documentation
- Comprehensive documentation in `/docs/`
- API references for all protocols
- Production deployment checklist
- Security hardening guides

### Testing
- 88%+ unit test coverage
- Integration test suite with Docker
- Benchmark suite for performance validation

---

## [0.1.0-alpha] - 2025-01-01

### Initial Development Release
- Core key-value storage
- Basic Raft consensus
- gRPC API
- Foundation for future development

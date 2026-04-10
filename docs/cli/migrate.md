# quicbaas migrate

Data migration operations for QuicBaaS.

## Overview

Commands for managing data migration between storage systems with support for verification, rate limiting, and progress tracking.

### Storage URLs

Storage URLs use the format: `scheme://path`

Supported schemes:
- `pebble://path/to/storage` - PebbleDB storage
- `memory://` - In-memory storage
- `file://path/to/storage` - File-based storage (using pebble)

## Commands

### start

Start migrating data from a source storage to a destination storage.

```bash
quicbaas migrate start --source <url> --dest <url>
```

#### Flags
- `--source string`: Source storage URL (required)
- `--dest string`: Destination storage URL (required)
- `--batch-size int`: Number of keys per batch (default: 1000)
- `--concurrency int`: Number of parallel workers (default: 4)
- `--verify`: Verify data after migration (default: true)
- `--delete-source`: Delete source data after successful migration
- `--rate-limit int`: Rate limit in KB/s (0 = unlimited)
- `--checkpoint duration`: Checkpoint interval (default: 30s)
- `--output string`: Output format (json, table) (default: "table")
- `--follow`: Follow migration progress until completion

#### Examples

```bash
# Basic migration
quicbaas migrate start --source pebble://./data/old --dest pebble://./data/new

# Migration with verification and rate limiting
quicbaas migrate start \
  --source pebble://./data/old \
  --dest pebble://./data/new \
  --verify \
  --rate-limit 10240 \
  --batch-size 1000

# Migration with automatic source deletion
quicbaas migrate start \
  --source pebble://./data/old \
  --dest pebble://./data/new \
  --verify \
  --delete-source

# Follow migration progress
quicbaas migrate start \
  --source pebble://./data/old \
  --dest pebble://./data/new \
  --follow
```

### status

Check the progress of an ongoing migration operation.

```bash
quicbaas migrate status
```

#### Flags
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Check migration status
quicbaas migrate status

# Check migration status with JSON output
quicbaas migrate status --output json
```

### cancel

Cancel an ongoing migration operation.

```bash
quicbaas migrate cancel
```

#### Flags
- `--rollback`: Rollback migrated data

#### Examples

```bash
# Cancel migration
quicbaas migrate cancel

# Cancel migration with rollback
quicbaas migrate cancel --rollback
```

## Migration Process

1. **Initialization**: Create migration manager and validate storage URLs
2. **Scanning**: Scan source storage for keys to migrate
3. **Batching**: Group keys into batches for efficient transfer
4. **Transfer**: Migrate batches using parallel workers
5. **Verification**: Verify data integrity (if enabled)
6. **Cleanup**: Delete source data (if requested)
7. **Completion**: Generate migration report

## Notes

- Migrations are atomic per batch
- Failed batches are automatically retried
- Checkpoints allow resuming interrupted migrations
- Rate limiting helps control bandwidth usage
- Verification compares checksums to ensure data integrity
- Rollback is a best-effort operation and may not restore exact previous state
- Progress includes: keys processed, bytes transferred, current rate, estimated time remaining

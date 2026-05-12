# Signal Bridge Integration Tests

End-to-end integration tests for go-signal + quicbaasd + signalbridge stack.

## Architecture

The Signal Bridge stack brings together:
- **go-signal** - Chat/messaging protocol
- **quicbaasd** - QuicBaaS daemon (KV store, event streaming)
- **signalbridge** - Bridge between signal protocol and quicbaasd

## Test Structure

- `docker-compose.yml` - Multi-container test environment
- `smoke_test.go` - Basic connectivity and bot command test

## Running Tests

```bash
make integration-test-signalbridge
```

This target:
1. Brings up the docker-compose stack
2. Waits for services to be healthy
3. Runs the smoke test
4. Tears down the stack

## Smoke Test Coverage

The initial smoke test verifies:
- All services start successfully
- Bot responds to `help` command
- Basic event flow works end-to-end

## Future Coverage

Additional tests to implement:
- Message delivery through signal bridge
- State synchronization between protocol and KV store
- Multi-user chat scenarios
- Persistence across restarts
- Error handling and recovery

See `test/integration/CLAUDE.md` for full integration testing conventions.

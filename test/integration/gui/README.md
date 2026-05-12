# GUI Integration Tests

Playwright-based end-to-end tests for the QuicBaaS web UI.

The canonical location of the Playwright test suite is:
- **`cmd/quicbaas/frontend/test/e2e/*.spec.ts`**

This is the Playwright convention - frontend tests live co-located with the frontend code.

The integration test target `make integration-test-gui` runs the Playwright suite against a live daemon at `http://localhost:18080/ui/`.

## Test Coverage

- `session_attach.spec.ts` - Multi-subscriber SSE, reconnection, event streaming
- `sidebar_nav.spec.ts` - Navigation and UI structure
- `asset_loading.spec.ts` - Static asset loading and caching

## Running Tests

```bash
# Auto-starts daemon, runs tests, stops daemon
make integration-test-gui

# Or manually (if daemon is already running)
cd cmd/quicbaas/frontend
QUICBAAS_UI_URL=http://localhost:18080/ui/ npx playwright test
```

## Configuration

Environment variables:
- `QUICBAAS_UI_URL` - Base URL of the web UI (default: `http://localhost:18080/ui/`)

See `cmd/quicbaas/frontend/playwright.config.ts` for timeout and retry settings.

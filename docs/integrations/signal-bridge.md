# Signal Messenger Bridge

Run a Signal bot backed by your QuicBaaS orchestrator. Operators chat with a normal Signal phone number; the bridge routes prompts into agent sessions and streams responses back. Encryption is end-to-end over the Signal Protocol; we use `go-libsignal` (PQXDH, Kyber, double ratchet) as a client, and an official-flavor `go-signal` server handles transport.

## Design Constraints

- **We do not modify `go-signal`.** The bridge registers as a normal client account against a running `go-signal` server, identical to any other Signal client. Upstream parity is deliberate so we track official Signal-Server behavior.
- The bridge is a separate process from the quicbaas daemon — one bridge per Signal account, scaling horizontally by running more bridges on different numbers.

## Components

- `pkg/signalbridge/http_transport.go` — REST + WebSocket client for the `go-signal` server. Uses `go-signal/pkg/api/rest` types and `go-signal/pkg/websocket.UnmarshalFrame` so wire shape tracks upstream exactly.
- `pkg/signalbridge/bot.go` — mapping between Signal inbound envelopes and orchestrator session events. Coalesces `assistant_chunk` streams into live-editable Signal messages, promotes large results to attachments, and enforces per-sender rate limits with exponential backoff.
- `pkg/signalbridge/registrar.go` — one-time account provisioning (verification code → account registration → pre-key upload).

## Deployment

1. Run a `go-signal` server (or use an existing one). The bridge needs a reachable base URL.
2. Register a Signal account for the bot:
   ```bash
   quicbaas signalbridge register \
     --server https://signal.example.com \
     --number +15551234567 \
     --captcha <captcha>
   ```
   After SMS verification, credentials are stored under `<datadir>/signalbridge/<number>/`.
3. Start the bridge pointing at your QuicBaaS daemon:
   ```bash
   quicbaas signalbridge run \
     --server https://signal.example.com \
     --number +15551234567 \
     --quicbaas https://quicbaas.internal:8443 \
     --tenant production
   ```

## User Experience

- **Prompt** — any direct message sent to the bot becomes a `prompt_submitted` event on a session keyed by (sender, conversation).
- **Streaming reply** — the bot sends an initial Signal message on first `assistant_chunk`, then edits it as more chunks arrive. Typing indicators are emitted throughout.
- **Tool calls** — shown as short interstitial messages ("Reading `/etc/hosts`…"), collapsed into the final reply on completion.
- **Large outputs** — results larger than 2 MB are uploaded as attachments; the reply links to them with a caption.
- **Rate limiting** — per-sender token bucket with exponential backoff on abuse. Returns a polite rate-limit notice rather than dropping silently.

## Exactly-Once Delivery

The bridge tracks `(session_id, sequence)` watermarks in local state, so reconnects after network hiccups replay from the last acknowledged event without duplicate Signal messages.

## Security

- All Signal traffic is E2E encrypted by `go-libsignal` — the bridge sees plaintext only in-memory between decryption and orchestrator dispatch.
- Bridge ↔ QuicBaaS uses OAuth2 client credentials with TLS; tokens are cached and refreshed on 401.
- Incoming senders are mapped to orchestrator operator identities through a configurable allowlist; unknown senders receive a fixed "unauthorized" reply.

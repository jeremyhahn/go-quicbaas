// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

// Package signalbridge implements a Signal messenger chat bridge that lets
// operators drive the QuicBaaS orchestrator via DMs and group chats on a
// Signal server.
//
// The bridge is a normal Signal client. It registers an account on a
// user-deployed go-signal server and uses go-libsignal for the Signal
// Protocol (double ratchet, sealed sender, pre-keys). Protocol primitives
// are not reimplemented.
//
// # Components
//
//   - SignalClient: wraps go-libsignal primitives and a pluggable
//     ServerTransport for HTTP/WebSocket interactions with a go-signal
//     server.
//   - OrchestratorBot: the main loop that converts inbound Signal messages
//     into orchestrator commands and forwards orchestrator session events
//     back out as Signal messages.
//   - IntentParser: handwritten command grammar recognizer.
//   - EventTranslator: converts orchestration.SessionEvent values into
//     Signal messages with assistant-chunk coalescing via the Edit
//     primitive so long responses produce one rolling message instead of
//     many.
//   - SessionManager: per-conversation state tracking (attached session,
//     operator identity, verbosity preferences).
//   - AuthBridge: maps a Signal identity to a QuicBaaS operator identity
//     under one of three policy modes.
//   - StateStore: JSON-file persistence for per-conversation state and
//     exactly-once delivery watermarks.
//
// # Robustness properties
//
//   - Exactly-once delivery to Signal is achieved by tagging outbound
//     messages with `(session_id, event_sequence)` and persisting the
//     last successfully sent sequence per conversation. On restart the
//     bot resumes from that watermark rather than replaying events.
//   - Pre-key inventory is maintained by a background loop that rotates
//     when the server reports low inventory, with configurable thresholds.
//   - Rate limiting respects 429 responses with exponential backoff.
//   - Session corruption recovery sends a Signal Protocol session-reset
//     marker and initiates a fresh double ratchet handshake.
package signalbridge

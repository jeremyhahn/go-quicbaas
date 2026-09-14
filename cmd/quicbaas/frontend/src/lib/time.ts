// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * Shared timestamp formatting utilities.
 *
 * The QuicBaaS daemon emits timestamps in three different units depending on
 * the surface (REST handlers, gRPC services, gateway translations):
 *   - seconds      e.g. 1_778_017_609                  (~1e9, std Unix seconds)
 *   - milliseconds e.g. 1_778_017_609_149              (~1e12, JS Date-native)
 *   - nanoseconds  e.g. 1_778_017_609_149_476_600      (~1e18, Go time.UnixNano)
 *
 * Multiplying nanoseconds by 1000 — the previous per-component pattern — pushed
 * values past Number.MAX_SAFE_INTEGER and produced "Invalid Date". These helpers
 * detect the unit by magnitude and produce a JS-millisecond value safely.
 *
 * Detection thresholds (all in absolute value):
 *   |t| < 1e11   → seconds      (covers up through year ~5138)
 *   |t| < 1e14   → milliseconds (covers up through year ~5138 in ms)
 *   otherwise   → nanoseconds  (Go's UnixNano output range)
 */

/**
 * Convert any supported timestamp representation to JS milliseconds.
 *
 * Handles the three on-the-wire forms QuicBaaS surfaces produce:
 *   • numeric (seconds | milliseconds | nanoseconds — auto-detected by magnitude)
 *   • numeric string ("1778019596") — same auto-detection after coercion
 *   • ISO 8601 string ("2026-05-05T22:22:57.000Z") — SDK toISO() wrappers emit this
 *
 * Returns null for null, undefined, empty string, zero, or any value that
 * doesn't parse — the formatter wrappers turn that into an em-dash.
 */
export function toMillis(ts: number | string | null | undefined): number | null {
  if (ts === null || ts === undefined || ts === '') return null;
  if (typeof ts === 'string') {
    // Numeric string → fall through to magnitude detection below.
    // Otherwise treat as a Date-parsable string (ISO 8601, RFC 2822, etc.).
    const asNum = Number(ts);
    if (Number.isFinite(asNum)) {
      ts = asNum;
    } else {
      const parsed = Date.parse(ts);
      return Number.isFinite(parsed) ? parsed : null;
    }
  }
  if (!Number.isFinite(ts) || ts === 0) return null;
  const abs = Math.abs(ts);
  if (abs < 1e11) return ts * 1000;          // seconds → ms
  if (abs < 1e14) return ts;                  // already ms
  return ts / 1_000_000;                      // ns → ms
}

/**
 * hasInstant reports whether a wire timestamp names a real point in time.
 *
 * Go's zero `time.Time` reaches the browser as a date, not as an absent field:
 * `encoding/json`'s `omitempty` does not apply to structs, so a `time.Time`
 * that was never set is written verbatim as "0001-01-01T00:00:00Z", and the
 * formatters above render it, faithfully, as "12/31/1". Measured on the
 * burn-in node: a task with no deferral showed "Retry not before 12/31/1",
 * which reads as a fact about the task and is not one.
 *
 * Matching on the year is exact for the value the server actually sends and
 * cannot mistake a real timestamp for it: nothing this system records
 * happened in year one.
 *
 * Numeric timestamps (Go int64 UnixNano fields such as `retry_not_before`)
 * carry their absence as 0, which `omitempty` drops entirely; either way the
 * field names no instant.
 */
export function hasInstant(ts: number | string | null | undefined): boolean {
  if (ts === null || ts === undefined || ts === '') return false;
  if (typeof ts === 'number') return Number.isFinite(ts) && ts > 0;
  return !ts.startsWith('0001-01-01');
}

/** Render a timestamp as "5/5/2026, 5:46:49 PM" or em-dash if missing/invalid. */
export function formatDateTime(ts: number | string | null | undefined): string {
  const ms = toMillis(ts);
  if (ms === null) return '—';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString();
}

/** Render a timestamp as "5:46:49 PM" or em-dash if missing/invalid. */
export function formatTimeOnly(ts: number | string | null | undefined): string {
  const ms = toMillis(ts);
  if (ms === null) return '—';
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString();
}

/** Render a relative duration like "5m ago" / "2h ago" from a timestamp. */
export function formatRelative(ts: number | string | null | undefined): string {
  const ms = toMillis(ts);
  if (ms === null) return '—';
  const delta = Date.now() - ms;
  if (delta < 0) return 'just now';
  const sec = Math.floor(delta / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  return `${days}d ago`;
}

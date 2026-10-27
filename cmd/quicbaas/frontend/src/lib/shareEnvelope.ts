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

// shareEnvelopeVersion is the only envelope format qrdb's canonical parser
// (pkg/ceremony/envelope.go ParseShareEnvelope) accepts.
const shareEnvelopeVersion = 1;

/** ShareEnvelopeFormatError reports claimed bytes that are not a share envelope. */
export class ShareEnvelopeFormatError extends Error {
  constructor(detail: string) {
    super(`The claimed envelope is not a share envelope: ${detail}`);
    this.name = 'ShareEnvelopeFormatError';
  }
}

/**
 * decodeShareEnvelope returns the bytes of a claimed share envelope. The REST
 * claim response carries the envelope as a Go []byte, which JSON encodes as
 * base64; the saved file must hold the decoded bytes, the canonical envelope
 * JSON the holder's qrdb tooling parses. The bytes are checked against that
 * parser's rules so a file that it would refuse is never offered as saved.
 */
export function decodeShareEnvelope(wire: string): Uint8Array<ArrayBuffer> {
  let binary: string;
  try {
    binary = atob(wire);
  } catch {
    throw new ShareEnvelopeFormatError('the field is not base64');
  }
  const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));

  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(bytes));
  } catch {
    throw new ShareEnvelopeFormatError('the bytes are not JSON');
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new ShareEnvelopeFormatError('the JSON is not an object');
  }
  const env = parsed as { version?: unknown; ciphertext?: unknown };
  if (env.version !== shareEnvelopeVersion) {
    throw new ShareEnvelopeFormatError(`version ${String(env.version)} is not ${shareEnvelopeVersion}`);
  }
  if (typeof env.ciphertext !== 'string' || env.ciphertext === '') {
    throw new ShareEnvelopeFormatError('it has no ciphertext');
  }
  return bytes;
}

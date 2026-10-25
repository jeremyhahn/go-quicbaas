/*
 * Copyright (c) 2025 Jeremy Hahn
 * Copyright (c) 2025 Automate The Things, LLC
 *
 * This file is part of go-quicbaas.
 */

// challengeMessage builds the exact bytes a key holder signs, mirroring
// go-qrdb's canonical helpers. A holder never signs a bare nonce: qrdb binds
// a domain-separation prefix and the challenge's context into the message,
// so a signature over the nonce alone is refused.

import type { BarrierQuorumChallenge } from './types.ts';

/** MTLS_CHALLENGE_CONTEXT mirrors qrdb mtls.ChallengeContext. */
export const MTLS_CHALLENGE_CONTEXT = 'qrdb/mtls-challenge/v1';

/** METHOD_DATA_PURPOSE mirrors qrdb mtls.MethodDataPurpose. */
export const METHOD_DATA_PURPOSE = 'purpose';

/** ChallengeMessageError reports a challenge no signing message can be built for. */
export class ChallengeMessageError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ChallengeMessageError';
  }
}

/** quorumChallengePurpose returns the purpose qrdb bound the challenge to. */
export function quorumChallengePurpose(challenge: BarrierQuorumChallenge): string {
  const purpose = challenge.method_data?.[METHOD_DATA_PURPOSE] ?? '';
  if (purpose === '') {
    throw new ChallengeMessageError(
      `challenge ${challenge.challenge_id} carries no purpose; it cannot be signed`,
    );
  }
  return purpose;
}

/**
 * quorumChallengeMessage returns, base64-encoded, the message qrdb's mtls
 * factor verifies a quorum answer over (mtls.ChallengeMessage): the context,
 * the challenge ID, the purpose and the nonce bytes, NUL-separated.
 */
export function quorumChallengeMessage(challenge: BarrierQuorumChallenge): string {
  const purpose = quorumChallengePurpose(challenge);
  const nonce = decodeBase64(challenge.nonce, 'nonce');
  return encodeBase64(
    nulJoin([text(MTLS_CHALLENGE_CONTEXT), text(challenge.challenge_id), text(purpose), nonce]),
  );
}

function text(s: string): Uint8Array {
  return new TextEncoder().encode(s);
}

function nulJoin(parts: Uint8Array[]): Uint8Array {
  const size = parts.reduce((n, p) => n + p.length, parts.length - 1);
  const out = new Uint8Array(size);
  let offset = 0;
  parts.forEach((part, i) => {
    if (i > 0) {
      out[offset] = 0;
      offset += 1;
    }
    out.set(part, offset);
    offset += part.length;
  });
  return out;
}

function decodeBase64(b64: string, field: string): Uint8Array {
  let binary: string;
  try {
    binary = atob(b64);
  } catch {
    throw new ChallengeMessageError(`challenge ${field} is not valid base64`);
  }
  return Uint8Array.from(binary, (c) => c.charCodeAt(0));
}

function encodeBase64(bytes: Uint8Array): string {
  return btoa(Array.from(bytes, (b) => String.fromCharCode(b)).join(''));
}

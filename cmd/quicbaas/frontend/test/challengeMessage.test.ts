import { describe, expect, it } from 'vitest';
import {
  ChallengeMessageError,
  quorumChallengeMessage,
  type BarrierQuorumChallenge,
} from '@quicbaas/svelte-security';

// fixture is a quorum challenge as qrdb issues it: the purpose rides in
// method_data and the nonce is base64 ("nonce").
const fixture: BarrierQuorumChallenge = {
  challenge_id: 'ch-1',
  factor: 'mtls',
  nonce: 'bm9uY2U=',
  method_data: { purpose: 'system/unseal/unseal' },
  expires_at: '2026-10-25T00:05:00Z',
};

describe('quorumChallengeMessage', () => {
  it('encodes the challenge exactly as qrdb mtls.ChallengeMessage does', () => {
    // "qrdb/mtls-challenge/v1" NUL "ch-1" NUL "system/unseal/unseal" NUL "nonce"
    expect(quorumChallengeMessage(fixture)).toBe(
      'cXJkYi9tdGxzLWNoYWxsZW5nZS92MQBjaC0xAHN5c3RlbS91bnNlYWwvdW5zZWFsAG5vbmNl',
    );
  });

  it('keeps binary nonce bytes intact', () => {
    const nonce = new Uint8Array([0x00, 0xff, 0x80, 0x7f]);
    const challenge = { ...fixture, nonce: btoa(String.fromCharCode(...nonce)) };
    const message = Uint8Array.from(atob(quorumChallengeMessage(challenge)), (c) => c.charCodeAt(0));
    expect(Array.from(message.slice(-nonce.length))).toEqual(Array.from(nonce));
  });

  it('refuses a challenge with no purpose', () => {
    expect(() => quorumChallengeMessage({ ...fixture, method_data: undefined })).toThrow(ChallengeMessageError);
    expect(() => quorumChallengeMessage({ ...fixture, method_data: { purpose: '' } })).toThrow(/no purpose/);
  });

  it('refuses a nonce that is not base64', () => {
    expect(() => quorumChallengeMessage({ ...fixture, nonce: '!!not base64!!' })).toThrow(ChallengeMessageError);
  });
});

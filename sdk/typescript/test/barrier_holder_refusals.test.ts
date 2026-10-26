// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The QUIC and gRPC-Web clients serve no barrier verb: the key-holder and
// quorum verbs refuse with each transport's typed not-implemented error, as
// their sibling barrier verbs do, and send nothing.

import { describe, it, expect } from 'vitest';
import { GrpcWebClient, GrpcWebNotImplementedError } from '../src/grpc_web.js';
import { QuicClient, QuicNotImplementedError } from '../src/quic.js';
import type { BarrierClient, Client } from '../src/interfaces.js';

const transports: Array<{ name: string; make: () => Client; error: new (...args: never[]) => Error }> = [
  { name: 'quic', make: () => new QuicClient('https://node.example:8443'), error: QuicNotImplementedError },
  { name: 'grpc-web', make: () => new GrpcWebClient('http://node.example'), error: GrpcWebNotImplementedError },
];

const verbs: Array<{ verb: string; call: (b: BarrierClient) => Promise<unknown> }> = [
  { verb: 'officers', call: (b) => b.officers() },
  { verb: 'registerOfficer', call: (b) => b.registerOfficer({ username: 'officer-1', csr_pem: 'CSR' }) },
  { verb: 'shareClaimBegin', call: (b) => b.shareClaimBegin('officer-1') },
  {
    verb: 'shareClaimComplete',
    call: (b) => b.shareClaimComplete({ username: 'officer-1', nonce_hex: 'cafe', signature: 'c2ln' }),
  },
  { verb: 'quorumBegin', call: (b) => b.quorumBegin({ domain: 'unseal', op: 'unseal', username: 'officer-1' }) },
  {
    verb: 'quorumSubmit',
    call: (b) =>
      b.quorumSubmit({
        domain: 'unseal',
        op: 'unseal',
        username: 'officer-1',
        challenge_id: 'ch-1',
        factor: 'mtls',
        response: 'c2ln',
        share: 'c2hhcmU=',
      }),
  },
  { verb: 'rekeyStart', call: (b) => b.rekeyStart({ domain: 'unseal', threshold: 2 }) },
];

describe.each(transports)('$name barrier key-holder verbs', ({ make, error }) => {
  it.each(verbs)('$verb throws the typed not-implemented error', async ({ verb, call }) => {
    const err = await call(make().barrier).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(error);
    expect((err as Error).message).toContain(verb);
  });
});

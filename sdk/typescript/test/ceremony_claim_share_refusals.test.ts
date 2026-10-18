// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The QUIC and gRPC-Web clients serve no ceremony verb: the share-claim pair
// refuses with each transport's typed not-implemented error, as its sibling
// ceremony verbs do, and sends nothing.

import { describe, it, expect } from 'vitest';
import { GrpcWebClient, GrpcWebNotImplementedError } from '../src/grpc_web.js';
import { QuicClient, QuicNotImplementedError } from '../src/quic.js';
import type { Client } from '../src/interfaces.js';

const transports: Array<{ name: string; make: () => Client; error: new (...args: never[]) => Error }> = [
  { name: 'quic', make: () => new QuicClient('https://node.example:8443'), error: QuicNotImplementedError },
  { name: 'grpc-web', make: () => new GrpcWebClient('http://node.example'), error: GrpcWebNotImplementedError },
];

describe.each(transports)('$name ceremony share claim', ({ make, error }) => {
  it('claimShareBegin throws the typed not-implemented error', async () => {
    const err = await make().ceremony.claimShareBegin({ username: 'officer-1' }).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(error);
    expect((err as Error).message).toContain('claimShareBegin');
  });

  it('claimShareComplete throws the typed not-implemented error', async () => {
    const err = await make()
      .ceremony.claimShareComplete({ username: 'officer-1', nonce_hex: 'cafe', signature: 'c2ln' })
      .catch((e: unknown) => e);

    expect(err).toBeInstanceOf(error);
    expect((err as Error).message).toContain('claimShareComplete');
  });
});

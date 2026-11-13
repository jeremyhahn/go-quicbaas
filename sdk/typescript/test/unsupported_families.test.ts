// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// gRPC-Web, Unix and Wails cannot carry projects, sdlc or accounts today (see
// docs/software-factory/api.md for why). Each family is still an object of
// its declared type whose every verb rejects with TransportNotSupportedError
// naming the transport, the family, the verb and the reason — not a null that
// fails with "cannot read properties of null" far from the cause.

import { describe, it, expect } from 'vitest';
import { GrpcWebClient } from '../src/grpc_web';
import { UnixClient } from '../src/unix';
import { WailsClient } from '../src/wails';
import { TransportNotSupportedError } from '../src/errors';
import { unsupportedClient } from '../src/unsupported';
import type { Client } from '../src/interfaces';
import type { ProjectsClient } from '../src/projects';

const transports: Array<{ name: string; client: () => Client; reason: string }> = [
  { name: 'grpc-web', client: () => new GrpcWebClient('http://node.example'), reason: 'no gRPC-Web endpoint' },
  { name: 'unix', client: () => new UnixClient('/run/quicbaas.sock'), reason: 'no Node gRPC client' },
  { name: 'wails', client: () => new WailsClient(), reason: 'no Wails binding' },
];

describe.each(transports)('$name transport — projects, sdlc and accounts', ({ name, client, reason }) => {
  it('rejects every family verb with the transport, family, verb and reason', async () => {
    const c = client();
    const calls: Array<[string, string, Promise<unknown>]> = [
      ['projects', 'createProject', c.projects.createProject({ tenant_id: 'tenant-a', name: 'api' })],
      ['sdlc', 'binding', c.sdlc.binding('tenant-a', 'proj-1')],
      ['accounts', 'listAccountsPage', c.accounts.listAccountsPage({ tenant_id: 'tenant-a' })],
      ['prompts', 'listPrompts', c.prompts.listPrompts('sdlc')],
    ];

    for (const [family, method, call] of calls) {
      const err = await call.catch((e: unknown) => e);
      expect(err).toBeInstanceOf(TransportNotSupportedError);
      const refusal = err as TransportNotSupportedError;
      expect(refusal.transport).toBe(name);
      expect(refusal.family).toBe(family);
      expect(refusal.method).toBe(method);
      expect(refusal.reason).toContain(reason);
      expect(refusal.message).toBe(`${name} transport does not carry ${family}.${method}: ${refusal.reason}`);
    }
  });

  it('is not mistaken for a promise when awaited', async () => {
    const c = client();
    const projects = await Promise.resolve(c.projects);
    expect(projects).toBe(c.projects);
  });
});

describe('unsupportedClient', () => {
  it('names the verb that was called', async () => {
    const projects = unsupportedClient<ProjectsClient>('test', 'projects', 'because');
    const err = await projects.deleteProject('tenant-a', 'proj-1').catch((e: unknown) => e);
    expect((err as TransportNotSupportedError).method).toBe('deleteProject');
    expect((err as TransportNotSupportedError).message).toBe('test transport does not carry projects.deleteProject: because');
  });

  it('answers nothing for symbol lookups, so inspection does not throw', () => {
    const projects = unsupportedClient<ProjectsClient>('test', 'projects', 'because');
    expect((projects as unknown as Record<symbol, unknown>)[Symbol.iterator]).toBeUndefined();
    expect(String(Object.keys(projects))).toBe('');
  });
});

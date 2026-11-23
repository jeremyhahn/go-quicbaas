// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// gRPC-Web, Unix and Wails cannot carry projects, sdlc, accounts or prompts today (see
// docs/software-factory/api.md for why). Each family is still an object of
// its declared type whose every verb rejects — not a null that fails with
// "cannot read properties of null" far from the cause. projects, sdlc and
// accounts reject with TransportNotSupportedError, naming the transport, the
// family, the verb and the reason, on gRPC-Web, Unix and Wails. Prompts do the
// same on Unix and Wails, but reject with GrpcWebNotImplementedError on
// gRPC-Web, as docs/SPEC.md (multiprotocol-api) requires.

import { describe, it, expect } from 'vitest';
import { GrpcWebClient, GrpcWebNotImplementedError } from '../src/grpc_web';
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

describe.each(transports)('$name transport — projects, sdlc and accounts (and prompts on unix and wails)', ({ name, client, reason }) => {
  it('rejects every family verb with the transport, family, verb and reason', async () => {
    const c = client();
    const calls: Array<[string, string, Promise<unknown>]> = [
      ['projects', 'createProject', c.projects.createProject({ tenant_id: 'tenant-a', name: 'api' })],
      ['sdlc', 'binding', c.sdlc.binding('tenant-a', 'proj-1')],
      ['accounts', 'listAccountsPage', c.accounts.listAccountsPage({ tenant_id: 'tenant-a' })],
    ];
    if (name !== 'grpc-web') {
      calls.push(['prompts', 'getEffectivePrompt', c.prompts.getEffectivePrompt({ set: 'sdlc', name: 'plan' })]);
    }

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

describe('gRPC-Web prompts', () => {
  it('gRPC-Web prompts reject every verb with GrpcWebNotImplementedError', async () => {
    const p = new GrpcWebClient('http://node.example').prompts;
    const addr = { set: 'sdlc', name: 'plan' };
    const calls: Array<[string, () => Promise<unknown>]> = [
      ['listPrompts', () => p.listPrompts({ set: 'sdlc' })],
      ['getPrompt', () => p.getPrompt(addr)],
      ['getEffectivePrompt', () => p.getEffectivePrompt(addr)],
      ['diffPrompt', () => p.diffPrompt(addr)],
      ['getPromptHistory', () => p.getPromptHistory(addr)],
      ['putPrompt', () => p.putPrompt({ ...addr, content: 'x', note: 'n', expected_version: 1 })],
      ['rollbackPrompt', () => p.rollbackPrompt({ ...addr, to_version: 1, expected_version: 2 })],
      ['resetPrompt', () => p.resetPrompt({ ...addr, expected_version: 1 })],
    ];

    for (const [verb, call] of calls) {
      const err = await call().catch((e: unknown) => e);
      expect(err).toBeInstanceOf(GrpcWebNotImplementedError);
      expect(err).not.toBeInstanceOf(TransportNotSupportedError);
      expect((err as GrpcWebNotImplementedError).message).toContain(`prompts.${verb}`);
    }
  });

  it('is not mistaken for a promise when awaited', async () => {
    const p = new GrpcWebClient('http://node.example').prompts;
    expect(await Promise.resolve(p)).toBe(p);
    expect((p as unknown as Record<symbol, unknown>)[Symbol.iterator]).toBeUndefined();
  });
});

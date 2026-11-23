// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// Pins the removal of the SDK's own seal client. The barrier is go-qrdb's;
// callers use client.barrier, and the SDK has no seal accessor or SealClient.

import { describe, it, expect } from 'vitest';
import { RestClient } from '../src/rest';
import * as sdk from '../src/index';

describe('no seal client', () => {
  it('RestClient has no seal accessor; client.barrier is the barrier', () => {
    const client = new RestClient('http://localhost');
    expect('seal' in client).toBe(false);
    expect(client.barrier).toBeDefined();
  });

  it('the package exports no SealClient or RestSealClient', () => {
    expect('SealClient' in sdk).toBe(false);
    expect('RestSealClient' in sdk).toBe(false);
  });
});

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

import { TransportNotSupportedError } from './errors.js';

// unsupportedClient returns a client of type T whose every method rejects
// with a TransportNotSupportedError naming the transport, the family and why.
// It stands in for a family a transport cannot carry, so `client.projects`
// is always an object of the declared type and a call fails with the reason
// rather than with "cannot read properties of null".
//
// `then` reads as undefined so that awaiting the client itself (or returning
// it from an async function) does not mistake it for a promise.
export function unsupportedClient<T extends object>(transport: string, family: string, reason: string): T {
  return new Proxy({} as T, {
    get(_target, prop) {
      if (typeof prop !== 'string' || prop === 'then') return undefined;
      return async () => {
        throw new TransportNotSupportedError(transport, family, reason, prop);
      };
    },
  });
}

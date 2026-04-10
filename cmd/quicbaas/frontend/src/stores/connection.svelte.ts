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
 * Connection state store.
 *
 * Tracks connectivity to the QuicBaaS daemon including address,
 * transport protocol, and error state.
 */

export interface ConnectionState {
  connected: boolean;
  address: string;
  transport: string;
  error: string;
}

let connected = $state(false);
let address = $state('');
let transport = $state('rest');
let error = $state('');

export const connectionStore = {
  get connected() { return connected; },
  get address() { return address; },
  get transport() { return transport; },
  get error() { return error; },
};

export function setConnected(addr: string, trans: string): void {
  connected = true;
  address = addr;
  transport = trans;
  error = '';
}

export function setDisconnected(): void {
  connected = false;
  address = '';
  error = '';
}

export function setConnectionError(err: string): void {
  connected = false;
  error = err;
}

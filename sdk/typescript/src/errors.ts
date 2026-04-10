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

// QuicBaaSError is the base error class for all SDK errors.
export class QuicBaaSError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'QuicBaaSError';
  }
}

// ConnectionError indicates a failure to establish or maintain a connection.
export class ConnectionError extends QuicBaaSError {
  constructor(message: string) {
    super(message);
    this.name = 'ConnectionError';
  }
}

// NotConnectedError indicates an operation was attempted before connecting.
export class NotConnectedError extends QuicBaaSError {
  constructor() {
    super('Not connected to QuicBaaS server');
    this.name = 'NotConnectedError';
  }
}

// APIError indicates a non-2xx HTTP response from the REST API.
export class APIError extends QuicBaaSError {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'APIError';
    this.status = status;
  }
}

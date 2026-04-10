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

import type { Client } from './interfaces.js';
import type { ClientConfig } from './types.js';

// WailsWindow extends the browser Window with the Wails runtime and Go bindings.
// Wails injects window.go and window.runtime at startup in desktop mode.
declare global {
  interface Window {
    go?: Record<string, Record<string, unknown>>;
    runtime?: {
      EventsOn(
        event: string,
        callback: (...args: unknown[]) => void,
      ): () => void;
      EventsOff(event: string): void;
    };
  }
}

// isWailsRuntime returns true when running inside a Wails desktop application.
// Wails injects window.go before any JS executes, so this check is synchronous.
export function isWailsRuntime(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.go !== 'undefined' &&
    window.go !== null
  );
}

// isDesktop is a semantic alias for isWailsRuntime.
export function isDesktop(): boolean {
  return isWailsRuntime();
}

// getClient returns the appropriate Client implementation based on the runtime
// environment. In a Wails desktop app the WailsClient is used, which delegates
// directly to Go bindings over IPC with no HTTP overhead. In a browser or Node
// environment the RestClient is used instead, making fetch() calls to the
// QuicBaaS REST API.
//
// Dynamic imports are used so that the unused transport is never bundled into
// the opposite environment's output.
export async function getClient(config?: ClientConfig): Promise<Client> {
  if (isWailsRuntime()) {
    const { WailsClient } = await import('./wails.js');
    return new WailsClient();
  }
  const { RestClient } = await import('./rest.js');
  return new RestClient(config?.baseUrl ?? '');
}

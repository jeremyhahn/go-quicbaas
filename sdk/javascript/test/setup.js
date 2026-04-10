/**
 * Jest test setup for QuicBaaS JavaScript SDK
 */

// Mock fetch for Node.js environment
if (typeof global.fetch === 'undefined') {
  global.fetch = jest.fn();
}

// Polyfill TextEncoder/TextDecoder for Node.js
import { TextEncoder, TextDecoder } from 'util';
if (typeof global.TextEncoder === 'undefined') {
  global.TextEncoder = TextEncoder;
}
if (typeof global.TextDecoder === 'undefined') {
  global.TextDecoder = TextDecoder;
}

// Polyfill btoa/atob for Node.js
if (typeof global.btoa === 'undefined') {
  global.btoa = (str) => Buffer.from(str, 'binary').toString('base64');
}
if (typeof global.atob === 'undefined') {
  global.atob = (str) => Buffer.from(str, 'base64').toString('binary');
}

// Mock AbortController
if (typeof global.AbortController === 'undefined') {
  global.AbortController = class AbortController {
    constructor() {
      this.signal = { aborted: false };
    }
    abort() {
      this.signal.aborted = true;
    }
  };
}

// Mock EventSource for SSE
if (typeof global.EventSource === 'undefined') {
  global.EventSource = class EventSource {
    constructor(url) {
      this.url = url;
      this.readyState = 0;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
    }
    close() {
      this.readyState = 2;
    }
  };
}

// Reset fetch mock before each test
beforeEach(() => {
  if (global.fetch.mockClear) {
    global.fetch.mockClear();
  }
});

// Increase timeout for async tests
jest.setTimeout(10000);

/**
 * QuicBaaS JavaScript SDK - Key-Value Client
 *
 * @module @quicbaas/client/clients/kv
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError, QuicBaaSError } from '../errors/index.js';
import {
  requiredString,
  required,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * Consistency levels for read operations.
 * @readonly
 * @enum {string}
 */
export const ConsistencyLevel = Object.freeze({
  /** Eventually consistent reads (fastest, may read stale data) */
  EVENTUAL: 'eventual',
  /** Linearizable reads (strongest, reads reflect all prior writes) */
  LINEARIZABLE: 'linearizable',
  /** Strong consistency (reads from leader) */
  STRONG: 'strong',
});

/**
 * Write modes for write operations.
 * @readonly
 * @enum {string}
 */
export const WriteMode = Object.freeze({
  /** Synchronous write (wait for replication) */
  SYNC: 'sync',
  /** Write to WAL before returning (durable but not replicated) */
  WAL_DURABLE: 'wal_durable',
  /** Batched write (fastest, may lose data on crash) */
  BATCHED: 'batched',
});

/**
 * Event types for watch operations.
 * @readonly
 * @enum {string}
 */
export const EventType = Object.freeze({
  /** Key was created or updated */
  PUT: 'PUT',
  /** Key was deleted */
  DELETE: 'DELETE',
});

/**
 * @typedef {Object} PutOptions
 * @property {number} [ttl] - Time-to-live in milliseconds
 * @property {string} [writeMode] - Write mode (sync, wal_durable, batched)
 * @property {string} [consistencyLevel] - Consistency level for the operation
 */

/**
 * @typedef {Object} GetOptions
 * @property {number} [revision] - Get value at specific revision
 * @property {string} [consistencyLevel] - Consistency level for the read
 */

/**
 * @typedef {Object} ListOptions
 * @property {string} [prefix] - Filter keys by prefix
 * @property {number} [limit] - Maximum number of keys to return
 * @property {string} [cursor] - Pagination cursor
 * @property {string} [consistencyLevel] - Consistency level for the read
 */

/**
 * @typedef {Object} WatchOptions
 * @property {number} [startRevision] - Start watching from specific revision
 * @property {boolean} [prevKV] - Include previous value in events
 * @property {boolean} [progressNotify] - Enable periodic progress notifications
 * @property {boolean} [filterPut] - Filter out PUT events
 * @property {boolean} [filterDelete] - Filter out DELETE events
 */

/**
 * @typedef {Object} PutResponse
 * @property {number} revision - The revision number after the put
 */

/**
 * @typedef {Object} GetResponse
 * @property {boolean} found - Whether the key was found
 * @property {*} value - The value (if found)
 * @property {number} revision - The revision number
 */

/**
 * @typedef {Object} DeleteResponse
 * @property {boolean} deleted - Whether the key was deleted
 * @property {number} revision - The revision number
 */

/**
 * @typedef {Object} ListResponse
 * @property {Array<string>} keys - The list of keys
 * @property {string} [nextCursor] - Cursor for next page (if more results)
 * @property {boolean} hasMore - Whether more results are available
 */

/**
 * @typedef {Object} WatchEvent
 * @property {string} type - Event type (PUT or DELETE)
 * @property {string} key - The key that changed
 * @property {*} value - The new value (for PUT events)
 * @property {*} [prevValue] - The previous value (if prevKV was enabled)
 * @property {number} revision - The revision number
 * @property {string} timestamp - ISO timestamp of the event
 */

/**
 * @typedef {Object} Watcher
 * @property {Function} close - Close the watcher and release resources
 * @property {boolean} closed - Whether the watcher is closed
 */

/**
 * Key-Value client for QuicBaaS.
 * Provides operations for storing, retrieving, and watching key-value pairs.
 */
class KVClient {
  /**
   * API path prefix for KV operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/kv';

  /**
   * Create a new KV client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  /**
   * Store a key-value pair.
   * @param {string} entityType - The entity type/namespace
   * @param {string} key - The key to store
   * @param {*} value - The value to store (will be JSON serialized)
   * @param {PutOptions} [options={}] - Put options
   * @returns {Promise<PutResponse>} The put response with revision
   * @throws {ValidationError} If required parameters are missing
   */
  async put(entityType, key, value, options = {}) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');
    required(value, 'value');

    this.#validatePutOptions(options);

    const path = this.#buildPath(entityType, key);
    const body = {
      value,
    };

    if (options.ttl !== undefined) {
      body.ttl = options.ttl;
    }

    const query = {};
    if (options.writeMode) {
      query.write_mode = options.writeMode;
    }
    if (options.consistencyLevel) {
      query.consistency = options.consistencyLevel;
    }

    const response = await this.#transport.put(path, body, { query });

    return {
      revision: response?.revision ?? 0,
    };
  }

  /**
   * Retrieve a value by key.
   * @param {string} entityType - The entity type/namespace
   * @param {string} key - The key to retrieve
   * @param {GetOptions} [options={}] - Get options
   * @returns {Promise<GetResponse>} The get response with value and metadata
   * @throws {ValidationError} If required parameters are missing
   */
  async get(entityType, key, options = {}) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');

    this.#validateGetOptions(options);

    const path = this.#buildPath(entityType, key);
    const query = {};

    if (options.revision !== undefined) {
      query.revision = options.revision;
    }
    if (options.consistencyLevel) {
      query.consistency = options.consistencyLevel;
    }

    const response = await this.#transport.get(path, { query });

    return {
      found: response?.found ?? false,
      value: response?.value ?? null,
      revision: response?.revision ?? 0,
    };
  }

  /**
   * Remove a key-value pair.
   * @param {string} entityType - The entity type/namespace
   * @param {string} key - The key to delete
   * @returns {Promise<DeleteResponse>} The delete response
   * @throws {ValidationError} If required parameters are missing
   */
  async delete(entityType, key) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');

    const path = this.#buildPath(entityType, key);
    const response = await this.#transport.delete(path);

    return {
      deleted: response?.deleted ?? false,
      revision: response?.revision ?? 0,
    };
  }

  /**
   * List keys matching criteria.
   * @param {string} entityType - The entity type/namespace
   * @param {ListOptions} [options={}] - List options
   * @returns {Promise<ListResponse>} The list response with keys and pagination
   * @throws {ValidationError} If required parameters are missing
   */
  async list(entityType, options = {}) {
    requiredString(entityType, 'entityType');

    this.#validateListOptions(options);

    const path = `${KVClient.API_PREFIX}/${encodeURIComponent(entityType)}`;
    const query = {};

    if (options.prefix) {
      query.prefix = options.prefix;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }
    if (options.consistencyLevel) {
      query.consistency = options.consistencyLevel;
    }

    const response = await this.#transport.get(path, { query });

    return {
      keys: response?.keys ?? [],
      nextCursor: response?.nextCursor ?? null,
      hasMore: response?.hasMore ?? false,
    };
  }

  /**
   * Watch a specific key for changes.
   * Returns a watcher object that can be used to receive events via callback.
   * @param {string} entityType - The entity type/namespace
   * @param {string} key - The key to watch
   * @param {WatchOptions & { onEvent: Function, onError?: Function }} options - Watch options with event callback
   * @returns {Promise<Watcher>} A watcher object with close() method
   * @throws {ValidationError} If required parameters are missing
   */
  async watch(entityType, key, options = {}) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');
    required(options.onEvent, 'options.onEvent');

    this.#validateWatchOptions(options);

    const path = `${KVClient.API_PREFIX}/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}/watch`;

    return this.#createWatcher(path, options, false);
  }

  /**
   * Watch all keys with a given prefix for changes.
   * Returns a watcher object that can be used to receive events via callback.
   * @param {string} entityType - The entity type/namespace
   * @param {string} prefix - The key prefix to watch
   * @param {WatchOptions & { onEvent: Function, onError?: Function }} options - Watch options with event callback
   * @returns {Promise<Watcher>} A watcher object with close() method
   * @throws {ValidationError} If required parameters are missing
   */
  async watchPrefix(entityType, prefix, options = {}) {
    requiredString(entityType, 'entityType');
    requiredString(prefix, 'prefix');
    required(options.onEvent, 'options.onEvent');

    this.#validateWatchOptions(options);

    const path = `${KVClient.API_PREFIX}/${encodeURIComponent(entityType)}/watch`;

    return this.#createWatcher(path, { ...options, prefix }, true);
  }

  /**
   * Build the API path for a key.
   * @param {string} entityType - Entity type
   * @param {string} key - Key
   * @returns {string} The API path
   * @private
   */
  #buildPath(entityType, key) {
    return `${KVClient.API_PREFIX}/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`;
  }

  /**
   * Validate put options.
   * @param {PutOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validatePutOptions(options) {
    if (options.ttl !== undefined) {
      positiveInteger(options.ttl, 'options.ttl');
    }
    if (options.writeMode !== undefined) {
      oneOf(
        options.writeMode,
        Object.values(WriteMode),
        'options.writeMode',
      );
    }
    if (options.consistencyLevel !== undefined) {
      oneOf(
        options.consistencyLevel,
        Object.values(ConsistencyLevel),
        'options.consistencyLevel',
      );
    }
  }

  /**
   * Validate get options.
   * @param {GetOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateGetOptions(options) {
    if (options.revision !== undefined) {
      nonNegativeInteger(options.revision, 'options.revision');
    }
    if (options.consistencyLevel !== undefined) {
      oneOf(
        options.consistencyLevel,
        Object.values(ConsistencyLevel),
        'options.consistencyLevel',
      );
    }
  }

  /**
   * Validate list options.
   * @param {ListOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.consistencyLevel !== undefined) {
      oneOf(
        options.consistencyLevel,
        Object.values(ConsistencyLevel),
        'options.consistencyLevel',
      );
    }
  }

  /**
   * Validate watch options.
   * @param {WatchOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateWatchOptions(options) {
    if (options.startRevision !== undefined) {
      nonNegativeInteger(options.startRevision, 'options.startRevision');
    }
    if (typeof options.onEvent !== 'function') {
      throw new ValidationError('options.onEvent must be a function', 'options.onEvent');
    }
    if (options.onError !== undefined && typeof options.onError !== 'function') {
      throw new ValidationError('options.onError must be a function', 'options.onError');
    }
  }

  /**
   * Create a watcher using Server-Sent Events.
   * @param {string} path - API path for the watch endpoint
   * @param {WatchOptions & { onEvent: Function, onError?: Function, prefix?: string }} options - Watch options
   * @param {boolean} isPrefix - Whether this is a prefix watch
   * @returns {Promise<Watcher>} A watcher object
   * @private
   */
  async #createWatcher(path, options, isPrefix) {
    const queryParams = new URLSearchParams();

    if (isPrefix && options.prefix) {
      queryParams.set('prefix', options.prefix);
    }
    if (options.startRevision !== undefined) {
      queryParams.set('start_revision', String(options.startRevision));
    }
    if (options.prevKV) {
      queryParams.set('prev_kv', 'true');
    }
    if (options.progressNotify) {
      queryParams.set('progress_notify', 'true');
    }
    if (options.filterPut) {
      queryParams.set('filter_put', 'true');
    }
    if (options.filterDelete) {
      queryParams.set('filter_delete', 'true');
    }

    const queryString = queryParams.toString();
    const fullPath = queryString ? `${path}?${queryString}` : path;
    const url = `${this.#transport.baseUrl}${fullPath}`;

    const headers = {};
    if (this.#transport.authToken) {
      headers['Authorization'] = `Bearer ${this.#transport.authToken}`;
    }

    let eventSource;
    let closed = false;

    const watcher = {
      get closed() {
        return closed;
      },
      close: () => {
        if (!closed) {
          closed = true;
          if (eventSource) {
            eventSource.close();
          }
        }
      },
    };

    try {
      eventSource = new EventSource(url);

      eventSource.onmessage = (event) => {
        if (closed) {
          return;
        }

        try {
          const data = JSON.parse(event.data);
          const watchEvent = {
            type: data.type || EventType.PUT,
            key: data.key,
            value: data.value,
            prevValue: data.prevValue,
            revision: data.revision,
            timestamp: data.timestamp || new Date().toISOString(),
          };

          // Apply client-side filters (server should handle this, but be defensive)
          if (options.filterPut && watchEvent.type === EventType.PUT) {
            return;
          }
          if (options.filterDelete && watchEvent.type === EventType.DELETE) {
            return;
          }

          options.onEvent(watchEvent);
        } catch (parseError) {
          if (options.onError) {
            options.onError(new QuicBaaSError(
              'Failed to parse watch event',
              'PARSE_ERROR',
              { cause: parseError.message },
            ));
          }
        }
      };

      eventSource.onerror = (error) => {
        if (closed) {
          return;
        }

        if (options.onError) {
          options.onError(new QuicBaaSError(
            'Watch connection error',
            'WATCH_ERROR',
            { cause: error },
          ));
        }
      };

      return watcher;
    } catch (error) {
      throw new QuicBaaSError(
        'Failed to create watch connection',
        'WATCH_ERROR',
        { cause: error.message },
      );
    }
  }
}

export default KVClient;

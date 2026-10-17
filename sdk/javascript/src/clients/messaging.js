/**
 * QuicBaaS JavaScript SDK - Messaging Client
 *
 * @module @quicbaas/client/clients/messaging
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError, QuicBaaSError } from '../errors/index.js';
import {
  requiredString,
  required,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * Message codec types for publish operations.
 * @readonly
 * @enum {string}
 */
export const MessageCodec = Object.freeze({
  /** JSON-encoded payload */
  JSON: 'json',
  /** MessagePack-encoded payload */
  MSGPACK: 'msgpack',
  /** Raw binary payload */
  BINARY: 'binary',
});

/**
 * Event types received from messaging subscriptions.
 * @readonly
 * @enum {string}
 */
export const MessagingEventType = Object.freeze({
  /** Standard message event */
  MESSAGE: 'message',
  /** Delta/diff event */
  DELTA: 'delta',
  /** Error event from server */
  ERROR: 'error',
});

/**
 * @typedef {Object} Channel
 * @property {string} name - Channel name
 * @property {boolean} persistent - Whether the channel persists messages
 * @property {number} bufferSize - Maximum messages buffered in memory
 * @property {number} ttl - Time-to-live for messages in milliseconds
 * @property {number} subscriberCount - Current number of subscribers
 * @property {string} createdAt - ISO timestamp of channel creation
 */

/**
 * @typedef {Object} CreateChannelOptions
 * @property {boolean} [persistent] - Whether to persist messages to storage
 * @property {number} [bufferSize] - Maximum messages buffered in memory
 * @property {number} [ttl] - Time-to-live for messages in milliseconds
 */

/**
 * @typedef {Object} Message
 * @property {string} id - Unique message identifier
 * @property {string} channel - Channel name the message was published to
 * @property {*} payload - Message payload
 * @property {string} codec - Codec used for the payload
 * @property {string} [dedupKey] - Deduplication key (if provided)
 * @property {string} timestamp - ISO timestamp of when the message was published
 */

/**
 * @typedef {Object} DeltaPayload
 * @property {*} oldValue - The previous value
 * @property {*} newValue - The new value
 * @property {string} [codec] - Codec for encoding the delta
 */

/**
 * @typedef {Object} PublishOptions
 * @property {string} [codec] - Message codec (json, msgpack, binary)
 * @property {string} [dedupKey] - Deduplication key to prevent duplicate messages
 */

/**
 * @typedef {Object} PublishResponse
 * @property {string} id - Published message identifier
 * @property {string} channel - Channel name
 * @property {string} timestamp - ISO timestamp of publication
 */

/**
 * @typedef {Object} Stats
 * @property {number} totalChannels - Total number of channels
 * @property {number} totalSubscribers - Total number of active subscribers
 * @property {number} totalMessages - Total messages published
 * @property {number} messagesPerSecond - Current message throughput
 */

/**
 * @typedef {Object} DedupStats
 * @property {number} totalKeys - Total deduplication keys tracked
 * @property {number} duplicatesBlocked - Total duplicate messages blocked
 * @property {number} hitRate - Cache hit rate percentage
 */

/**
 * @typedef {Object} Subscription
 * @property {Function} close - Close the subscription and release resources
 * @property {boolean} closed - Whether the subscription has been closed
 * @property {string} [id] - Subscription identifier (set once the server assigns one)
 */

/**
 * @typedef {Object} SubscribeOptions
 * @property {Function} onMessage - Callback invoked for each received message
 * @property {Function} [onError] - Callback invoked on subscription errors
 */

/**
 * @typedef {Object} WatchKeyOptions
 * @property {Function} onEvent - Callback invoked for each watch event
 * @property {Function} [onError] - Callback invoked on watch errors
 */

/**
 * Messaging client for QuicBaaS.
 * Provides operations for pub/sub messaging with channels, subscriptions,
 * delta publishing, key-prefix watching, and statistics.
 */
class MessagingClient {
  /**
   * API path prefix for messaging operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/messaging';

  /**
   * Create a new Messaging client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  // ---------------------------------------------------------------------------
  // Channel operations
  // ---------------------------------------------------------------------------

  /**
   * Create a new messaging channel.
   * @param {string} name - Channel name
   * @param {CreateChannelOptions} [options={}] - Channel creation options
   * @returns {Promise<Channel>} The created channel
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async createChannel(name, options = {}) {
    requiredString(name, 'name');

    this.#validateCreateChannelOptions(options);

    const body = { name };

    if (options.persistent !== undefined) {
      body.persistent = options.persistent;
    }
    if (options.bufferSize !== undefined) {
      body.buffer_size = options.bufferSize;
    }
    if (options.ttl !== undefined) {
      body.ttl = options.ttl;
    }

    const path = `${MessagingClient.API_PREFIX}/channels`;
    const response = await this.#transport.post(path, body);

    return this.#mapChannelResponse(response);
  }

  /**
   * Delete a messaging channel.
   * @param {string} name - Channel name to delete
   * @returns {Promise<void>}
   * @throws {ValidationError} If name is missing or invalid
   */
  async deleteChannel(name) {
    requiredString(name, 'name');

    const path = `${MessagingClient.API_PREFIX}/channels/${encodeURIComponent(name)}`;
    await this.#transport.delete(path);
  }

  /**
   * Get information about a messaging channel.
   * @param {string} name - Channel name
   * @returns {Promise<Channel>} The channel information
   * @throws {ValidationError} If name is missing or invalid
   */
  async getChannel(name) {
    requiredString(name, 'name');

    const path = `${MessagingClient.API_PREFIX}/channels/${encodeURIComponent(name)}`;
    const response = await this.#transport.get(path);

    return this.#mapChannelResponse(response);
  }

  /**
   * List all messaging channels.
   * @returns {Promise<Channel[]>} Array of channels
   */
  async listChannels() {
    const path = `${MessagingClient.API_PREFIX}/channels`;
    const response = await this.#transport.get(path);

    const channels = response?.channels ?? response ?? [];

    if (!Array.isArray(channels)) {
      return [];
    }

    return channels.map((ch) => this.#mapChannelResponse(ch));
  }

  // ---------------------------------------------------------------------------
  // Publishing operations
  // ---------------------------------------------------------------------------

  /**
   * Publish a message to a channel.
   * @param {string} channelName - Channel to publish to
   * @param {*} payload - Message payload
   * @param {PublishOptions} [options={}] - Publish options
   * @returns {Promise<PublishResponse>} The publish response
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async publish(channelName, payload, options = {}) {
    requiredString(channelName, 'channelName');
    required(payload, 'payload');

    this.#validatePublishOptions(options);

    const body = { payload };

    if (options.codec !== undefined) {
      body.codec = options.codec;
    }
    if (options.dedupKey !== undefined) {
      body.dedup_key = options.dedupKey;
    }

    const path = `${MessagingClient.API_PREFIX}/channels/${encodeURIComponent(channelName)}/publish`;
    const response = await this.#transport.post(path, body);

    return {
      id: response?.id ?? '',
      channel: response?.channel ?? channelName,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Publish a delta (diff between old and new values) to a channel.
   * @param {string} channelName - Channel to publish to
   * @param {*} oldValue - Previous value
   * @param {*} newValue - New value
   * @param {Object} [options={}] - Delta publish options
   * @param {string} [options.codec] - Codec for encoding the delta
   * @returns {Promise<PublishResponse>} The publish response
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async publishDelta(channelName, oldValue, newValue, options = {}) {
    requiredString(channelName, 'channelName');
    required(oldValue, 'oldValue');
    required(newValue, 'newValue');

    if (options.codec !== undefined) {
      oneOf(options.codec, Object.values(MessageCodec), 'options.codec');
    }

    const body = {
      old_value: oldValue,
      new_value: newValue,
    };

    if (options.codec !== undefined) {
      body.codec = options.codec;
    }

    const path = `${MessagingClient.API_PREFIX}/channels/${encodeURIComponent(channelName)}/publish-delta`;
    const response = await this.#transport.post(path, body);

    return {
      id: response?.id ?? '',
      channel: response?.channel ?? channelName,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  // ---------------------------------------------------------------------------
  // Subscription operations
  // ---------------------------------------------------------------------------

  /**
   * Subscribe to a channel via Server-Sent Events.
   * Returns a subscription object that delivers messages through the onMessage
   * callback. Call subscription.close() to unsubscribe and release resources.
   * @param {string} channelName - Channel to subscribe to
   * @param {SubscribeOptions} options - Subscription options with callbacks
   * @returns {Promise<Subscription>} A subscription object with close() method
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async subscribe(channelName, options = {}) {
    requiredString(channelName, 'channelName');
    required(options.onMessage, 'options.onMessage');

    this.#validateCallbackOptions(options.onMessage, options.onError);

    const path = `${MessagingClient.API_PREFIX}/channels/${encodeURIComponent(channelName)}/subscribe`;

    return this.#createSSESubscription(path, options.onMessage, options.onError);
  }

  /**
   * Unsubscribe from a subscription by its server-assigned identifier.
   * This is an alternative to calling subscription.close() and can be used
   * to cancel a subscription from a different context.
   * @param {string} subscriptionId - Subscription identifier
   * @returns {Promise<void>}
   * @throws {ValidationError} If subscriptionId is missing or invalid
   */
  async unsubscribe(subscriptionId) {
    requiredString(subscriptionId, 'subscriptionId');

    const path = `${MessagingClient.API_PREFIX}/subscriptions/${encodeURIComponent(subscriptionId)}`;
    await this.#transport.delete(path);
  }

  // ---------------------------------------------------------------------------
  // Watch operations
  // ---------------------------------------------------------------------------

  /**
   * Watch a key prefix for changes via Server-Sent Events.
   * Returns a subscription object that delivers watch events through the
   * onEvent callback. Call subscription.close() to stop watching.
   * @param {string} prefix - Key prefix to watch
   * @param {WatchKeyOptions} options - Watch options with callbacks
   * @returns {Promise<Subscription>} A subscription object with close() method
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async watchPrefix(prefix, options = {}) {
    requiredString(prefix, 'prefix');
    required(options.onEvent, 'options.onEvent');

    this.#validateCallbackOptions(options.onEvent, options.onError);

    const path = `${MessagingClient.API_PREFIX}/watch/${encodeURIComponent(prefix)}`;

    return this.#createSSESubscription(path, options.onEvent, options.onError);
  }

  // ---------------------------------------------------------------------------
  // Stats operations
  // ---------------------------------------------------------------------------

  /**
   * Get messaging system statistics.
   * @returns {Promise<Stats>} Messaging statistics
   */
  async getStats() {
    const path = `${MessagingClient.API_PREFIX}/stats`;
    const response = await this.#transport.get(path);

    return {
      totalChannels: response?.totalChannels ?? response?.total_channels ?? 0,
      totalSubscribers: response?.totalSubscribers ?? response?.total_subscribers ?? 0,
      totalMessages: response?.totalMessages ?? response?.total_messages ?? 0,
      messagesPerSecond: response?.messagesPerSecond ?? response?.messages_per_second ?? 0,
    };
  }

  /**
   * Get deduplication statistics.
   * @returns {Promise<DedupStats>} Deduplication statistics
   */
  async getDedupStats() {
    const path = `${MessagingClient.API_PREFIX}/dedup-stats`;
    const response = await this.#transport.get(path);

    return {
      totalKeys: response?.totalKeys ?? response?.total_keys ?? 0,
      duplicatesBlocked: response?.duplicatesBlocked ?? response?.duplicates_blocked ?? 0,
      hitRate: response?.hitRate ?? response?.hit_rate ?? 0,
    };
  }

  // ---------------------------------------------------------------------------
  // Private helpers
  // ---------------------------------------------------------------------------

  /**
   * Validate channel creation options.
   * @param {CreateChannelOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateCreateChannelOptions(options) {
    if (options.bufferSize !== undefined) {
      positiveInteger(options.bufferSize, 'options.bufferSize');
    }
    if (options.ttl !== undefined) {
      positiveInteger(options.ttl, 'options.ttl');
    }
    if (options.persistent !== undefined && typeof options.persistent !== 'boolean') {
      throw new ValidationError('options.persistent must be a boolean', 'options.persistent');
    }
  }

  /**
   * Validate publish options.
   * @param {PublishOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validatePublishOptions(options) {
    if (options.codec !== undefined) {
      oneOf(options.codec, Object.values(MessageCodec), 'options.codec');
    }
    if (options.dedupKey !== undefined) {
      requiredString(options.dedupKey, 'options.dedupKey');
    }
  }

  /**
   * Validate callback function parameters.
   * @param {Function} onData - Primary data callback (required by caller)
   * @param {Function} [onError] - Optional error callback
   * @throws {ValidationError} If callbacks are not functions
   * @private
   */
  #validateCallbackOptions(onData, onError) {
    if (typeof onData !== 'function') {
      throw new ValidationError('callback must be a function', 'callback');
    }
    if (onError !== undefined && typeof onError !== 'function') {
      throw new ValidationError('onError must be a function', 'onError');
    }
  }

  /**
   * Map a raw channel response to a normalized Channel object.
   * @param {Object} response - Raw API response
   * @returns {Channel} Normalized channel object
   * @private
   */
  #mapChannelResponse(response) {
    return {
      name: response?.name ?? '',
      persistent: response?.persistent ?? false,
      bufferSize: response?.bufferSize ?? response?.buffer_size ?? 0,
      ttl: response?.ttl ?? 0,
      subscriberCount: response?.subscriberCount ?? response?.subscriber_count ?? 0,
      createdAt: response?.createdAt ?? response?.created_at ?? '',
    };
  }

  /**
   * Create a Server-Sent Events subscription.
   * Establishes an EventSource connection to the given path and routes
   * incoming events through the provided callbacks.
   * @param {string} path - API path for the SSE endpoint
   * @param {Function} onData - Callback invoked with parsed event data
   * @param {Function} [onError] - Callback invoked on connection or parse errors
   * @returns {Promise<Subscription>} A subscription object with close() method
   * @private
   */
  async #createSSESubscription(path, onData, onError) {
    const url = `${this.#transport.baseUrl}${path}`;

    let eventSource;
    let closed = false;
    let subscriptionId = null;

    const subscription = {
      get closed() {
        return closed;
      },
      get id() {
        return subscriptionId;
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

          if (data.subscription_id) {
            subscriptionId = data.subscription_id;
          }

          onData(data);
        } catch (parseError) {
          if (onError) {
            onError(new QuicBaaSError(
              'Failed to parse messaging event',
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

        if (onError) {
          onError(new QuicBaaSError(
            'Messaging subscription connection error',
            'SUBSCRIPTION_ERROR',
            { cause: error },
          ));
        }
      };

      return subscription;
    } catch (error) {
      throw new QuicBaaSError(
        'Failed to create messaging subscription',
        'SUBSCRIPTION_ERROR',
        { cause: error.message },
      );
    }
  }
}

export default MessagingClient;

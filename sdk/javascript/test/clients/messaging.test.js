/**
 * QuicBaaS JavaScript SDK - Messaging Client Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import MessagingClient, { MessageCodec, MessagingEventType } from '../../src/clients/messaging.js';
import { ValidationError, QuicBaaSError } from '../../src/errors/index.js';

describe('MessagingClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
      baseUrl: 'https://api.example.com',
      authToken: null,
    };
    client = new MessagingClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const messagingClient = new MessagingClient(mockTransport);
      expect(messagingClient).toBeInstanceOf(MessagingClient);
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new MessagingClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is undefined', () => {
      expect(() => new MessagingClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('exported constants', () => {
    it('should export MessageCodec enum', () => {
      expect(MessageCodec.JSON).toBe('json');
      expect(MessageCodec.MSGPACK).toBe('msgpack');
      expect(MessageCodec.BINARY).toBe('binary');
    });

    it('should export MessagingEventType enum', () => {
      expect(MessagingEventType.MESSAGE).toBe('message');
      expect(MessagingEventType.DELTA).toBe('delta');
      expect(MessagingEventType.ERROR).toBe('error');
    });

    it('should have frozen MessageCodec', () => {
      expect(Object.isFrozen(MessageCodec)).toBe(true);
    });

    it('should have frozen MessagingEventType', () => {
      expect(Object.isFrozen(MessagingEventType)).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------
  // Channel operations
  // ---------------------------------------------------------------------------

  describe('createChannel', () => {
    it('should create a channel with only a name', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'events',
        persistent: false,
        buffer_size: 100,
        ttl: 0,
        subscriber_count: 0,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.createChannel('events');

      expect(result).toEqual({
        name: 'events',
        persistent: false,
        bufferSize: 100,
        ttl: 0,
        subscriberCount: 0,
        createdAt: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
        { name: 'events' },
      );
    });

    it('should create a channel with persistent option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'audit',
        persistent: true,
        buffer_size: 0,
        ttl: 0,
        subscriber_count: 0,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.createChannel('audit', { persistent: true });

      expect(result.persistent).toBe(true);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
        { name: 'audit', persistent: true },
      );
    });

    it('should create a channel with bufferSize option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'buffered',
        persistent: false,
        buffer_size: 500,
        ttl: 0,
        subscriber_count: 0,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.createChannel('buffered', { bufferSize: 500 });

      expect(result.bufferSize).toBe(500);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
        { name: 'buffered', buffer_size: 500 },
      );
    });

    it('should create a channel with ttl option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'ephemeral',
        persistent: false,
        buffer_size: 0,
        ttl: 60000,
        subscriber_count: 0,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.createChannel('ephemeral', { ttl: 60000 });

      expect(result.ttl).toBe(60000);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
        { name: 'ephemeral', ttl: 60000 },
      );
    });

    it('should create a channel with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'full',
        persistent: true,
        buffer_size: 1000,
        ttl: 30000,
        subscriber_count: 0,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.createChannel('full', {
        persistent: true,
        bufferSize: 1000,
        ttl: 30000,
      });

      expect(result).toEqual({
        name: 'full',
        persistent: true,
        bufferSize: 1000,
        ttl: 30000,
        subscriberCount: 0,
        createdAt: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
        { name: 'full', persistent: true, buffer_size: 1000, ttl: 30000 },
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.createChannel('test');

      expect(result).toEqual({
        name: '',
        persistent: false,
        bufferSize: 0,
        ttl: 0,
        subscriberCount: 0,
        createdAt: '',
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.createChannel('test');

      expect(result).toEqual({
        name: '',
        persistent: false,
        bufferSize: 0,
        ttl: 0,
        subscriberCount: 0,
        createdAt: '',
      });
    });

    it('should handle response with camelCase field names', async () => {
      mockTransport.post.mockResolvedValueOnce({
        name: 'camel',
        persistent: true,
        bufferSize: 200,
        ttl: 5000,
        subscriberCount: 3,
        createdAt: '2025-01-17T12:00:00.000Z',
      });

      const result = await client.createChannel('camel');

      expect(result).toEqual({
        name: 'camel',
        persistent: true,
        bufferSize: 200,
        ttl: 5000,
        subscriberCount: 3,
        createdAt: '2025-01-17T12:00:00.000Z',
      });
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createChannel('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is null', async () => {
      await expect(client.createChannel(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is undefined', async () => {
      await expect(client.createChannel(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is not a string', async () => {
      await expect(client.createChannel(123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when bufferSize is negative', async () => {
      await expect(client.createChannel('test', { bufferSize: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when bufferSize is zero', async () => {
      await expect(client.createChannel('test', { bufferSize: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when bufferSize is not an integer', async () => {
      await expect(client.createChannel('test', { bufferSize: 3.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is negative', async () => {
      await expect(client.createChannel('test', { ttl: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is zero', async () => {
      await expect(client.createChannel('test', { ttl: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is not an integer', async () => {
      await expect(client.createChannel('test', { ttl: 1.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when persistent is not a boolean', async () => {
      await expect(client.createChannel('test', { persistent: 'yes' })).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.post.mockRejectedValueOnce(new Error('Network failure'));

      await expect(client.createChannel('test')).rejects.toThrow('Network failure');
    });
  });

  describe('deleteChannel', () => {
    it('should delete a channel successfully', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.deleteChannel('events');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/events',
      );
    });

    it('should URL encode the channel name', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.deleteChannel('my channel');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/my%20channel',
      );
    });

    it('should URL encode special characters in channel name', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.deleteChannel('topic/subtopic');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/topic%2Fsubtopic',
      );
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.deleteChannel('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is null', async () => {
      await expect(client.deleteChannel(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is undefined', async () => {
      await expect(client.deleteChannel(undefined)).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.delete.mockRejectedValueOnce(new Error('Not found'));

      await expect(client.deleteChannel('nonexistent')).rejects.toThrow('Not found');
    });
  });

  describe('getChannel', () => {
    it('should get channel information successfully', async () => {
      mockTransport.get.mockResolvedValueOnce({
        name: 'events',
        persistent: true,
        buffer_size: 200,
        ttl: 60000,
        subscriber_count: 5,
        created_at: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.getChannel('events');

      expect(result).toEqual({
        name: 'events',
        persistent: true,
        bufferSize: 200,
        ttl: 60000,
        subscriberCount: 5,
        createdAt: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/events',
      );
    });

    it('should URL encode the channel name', async () => {
      mockTransport.get.mockResolvedValueOnce({ name: 'my channel' });

      await client.getChannel('my channel');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/my%20channel',
      );
    });

    it('should handle response with camelCase field names', async () => {
      mockTransport.get.mockResolvedValueOnce({
        name: 'test',
        persistent: false,
        bufferSize: 100,
        subscriberCount: 2,
        createdAt: '2025-01-17T12:00:00.000Z',
      });

      const result = await client.getChannel('test');

      expect(result.bufferSize).toBe(100);
      expect(result.subscriberCount).toBe(2);
      expect(result.createdAt).toBe('2025-01-17T12:00:00.000Z');
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getChannel('test');

      expect(result).toEqual({
        name: '',
        persistent: false,
        bufferSize: 0,
        ttl: 0,
        subscriberCount: 0,
        createdAt: '',
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getChannel('test');

      expect(result).toEqual({
        name: '',
        persistent: false,
        bufferSize: 0,
        ttl: 0,
        subscriberCount: 0,
        createdAt: '',
      });
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.getChannel('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is null', async () => {
      await expect(client.getChannel(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is undefined', async () => {
      await expect(client.getChannel(undefined)).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.get.mockRejectedValueOnce(new Error('Server error'));

      await expect(client.getChannel('test')).rejects.toThrow('Server error');
    });
  });

  describe('listChannels', () => {
    it('should list channels from response with channels array', async () => {
      mockTransport.get.mockResolvedValueOnce({
        channels: [
          { name: 'events', persistent: true, buffer_size: 100, ttl: 0, subscriber_count: 3, created_at: '2025-01-17T00:00:00.000Z' },
          { name: 'logs', persistent: false, buffer_size: 50, ttl: 60000, subscriber_count: 1, created_at: '2025-01-17T01:00:00.000Z' },
        ],
      });

      const result = await client.listChannels();

      expect(result).toEqual([
        { name: 'events', persistent: true, bufferSize: 100, ttl: 0, subscriberCount: 3, createdAt: '2025-01-17T00:00:00.000Z' },
        { name: 'logs', persistent: false, bufferSize: 50, ttl: 60000, subscriberCount: 1, createdAt: '2025-01-17T01:00:00.000Z' },
      ]);
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/messaging/channels',
      );
    });

    it('should list channels from response that is a direct array', async () => {
      mockTransport.get.mockResolvedValueOnce([
        { name: 'ch1', persistent: false, buffer_size: 0, ttl: 0, subscriber_count: 0, created_at: '' },
      ]);

      const result = await client.listChannels();

      expect(result).toHaveLength(1);
      expect(result[0].name).toBe('ch1');
    });

    it('should return empty array for null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.listChannels();

      expect(result).toEqual([]);
    });

    it('should return empty array for undefined response', async () => {
      mockTransport.get.mockResolvedValueOnce(undefined);

      const result = await client.listChannels();

      expect(result).toEqual([]);
    });

    it('should return empty array for empty channels array', async () => {
      mockTransport.get.mockResolvedValueOnce({ channels: [] });

      const result = await client.listChannels();

      expect(result).toEqual([]);
    });

    it('should return empty array when response is non-array, non-object', async () => {
      mockTransport.get.mockResolvedValueOnce('invalid');

      const result = await client.listChannels();

      expect(result).toEqual([]);
    });

    it('should handle response with camelCase field names in channel list', async () => {
      mockTransport.get.mockResolvedValueOnce({
        channels: [
          { name: 'test', persistent: true, bufferSize: 200, ttl: 5000, subscriberCount: 7, createdAt: '2025-01-17T12:00:00.000Z' },
        ],
      });

      const result = await client.listChannels();

      expect(result[0]).toEqual({
        name: 'test',
        persistent: true,
        bufferSize: 200,
        ttl: 5000,
        subscriberCount: 7,
        createdAt: '2025-01-17T12:00:00.000Z',
      });
    });

    it('should propagate transport errors', async () => {
      mockTransport.get.mockRejectedValueOnce(new Error('Connection refused'));

      await expect(client.listChannels()).rejects.toThrow('Connection refused');
    });
  });

  // ---------------------------------------------------------------------------
  // Publishing operations
  // ---------------------------------------------------------------------------

  describe('publish', () => {
    it('should publish a message with only required parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-001',
        channel: 'events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publish('events', { action: 'click' });

      expect(result).toEqual({
        id: 'msg-001',
        channel: 'events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/events/publish',
        { payload: { action: 'click' } },
      );
    });

    it('should publish a message with codec option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-002',
        channel: 'binary-events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publish('binary-events', 'raw-data', { codec: MessageCodec.BINARY });

      expect(result).toEqual({
        id: 'msg-002',
        channel: 'binary-events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/binary-events/publish',
        { payload: 'raw-data', codec: 'binary' },
      );
    });

    it('should publish a message with json codec', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-003',
        channel: 'json-events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publish('json-events', { key: 'value' }, { codec: MessageCodec.JSON });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/json-events/publish',
        { payload: { key: 'value' }, codec: 'json' },
      );
    });

    it('should publish a message with msgpack codec', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-004',
        channel: 'msgpack-events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publish('msgpack-events', [1, 2, 3], { codec: MessageCodec.MSGPACK });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/msgpack-events/publish',
        { payload: [1, 2, 3], codec: 'msgpack' },
      );
    });

    it('should publish a message with dedupKey option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-005',
        channel: 'events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publish('events', 'data', { dedupKey: 'unique-key-123' });

      expect(result.id).toBe('msg-005');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/events/publish',
        { payload: 'data', dedup_key: 'unique-key-123' },
      );
    });

    it('should publish a message with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-006',
        channel: 'events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publish('events', { event: 'test' }, {
        codec: MessageCodec.JSON,
        dedupKey: 'dedup-abc',
      });

      expect(result).toEqual({
        id: 'msg-006',
        channel: 'events',
        timestamp: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/events/publish',
        { payload: { event: 'test' }, codec: 'json', dedup_key: 'dedup-abc' },
      );
    });

    it('should URL encode the channel name', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'msg-007',
        channel: 'my channel',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publish('my channel', 'data');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/my%20channel/publish',
        { payload: 'data' },
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.publish('events', 'data');

      expect(result.id).toBe('');
      expect(result.channel).toBe('events');
      expect(result.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.publish('events', 'data');

      expect(result.id).toBe('');
      expect(result.channel).toBe('events');
      expect(result.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    });

    it('should default channel to channelName when missing in response', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'msg-099' });

      const result = await client.publish('my-channel', 'data');

      expect(result.channel).toBe('my-channel');
    });

    it('should publish string payload', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'msg-str', channel: 'test', timestamp: '2025-01-17T00:00:00.000Z' });

      await client.publish('test', 'hello world');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/test/publish',
        { payload: 'hello world' },
      );
    });

    it('should publish numeric payload', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'msg-num', channel: 'test', timestamp: '2025-01-17T00:00:00.000Z' });

      await client.publish('test', 42);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/test/publish',
        { payload: 42 },
      );
    });

    it('should publish array payload', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'msg-arr', channel: 'test', timestamp: '2025-01-17T00:00:00.000Z' });

      await client.publish('test', [1, 2, 3]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/test/publish',
        { payload: [1, 2, 3] },
      );
    });

    it('should throw ValidationError when channelName is empty', async () => {
      await expect(client.publish('', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is null', async () => {
      await expect(client.publish(null, 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is undefined', async () => {
      await expect(client.publish(undefined, 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when payload is null', async () => {
      await expect(client.publish('events', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when payload is undefined', async () => {
      await expect(client.publish('events', undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when codec is invalid', async () => {
      await expect(client.publish('events', 'data', { codec: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when dedupKey is empty string', async () => {
      await expect(client.publish('events', 'data', { dedupKey: '' })).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.post.mockRejectedValueOnce(new Error('Channel not found'));

      await expect(client.publish('nonexistent', 'data')).rejects.toThrow('Channel not found');
    });
  });

  describe('publishDelta', () => {
    it('should publish a delta with only required parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'delta-001',
        channel: 'state-changes',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publishDelta('state-changes', { count: 1 }, { count: 2 });

      expect(result).toEqual({
        id: 'delta-001',
        channel: 'state-changes',
        timestamp: '2025-01-17T00:00:00.000Z',
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/state-changes/publish-delta',
        { old_value: { count: 1 }, new_value: { count: 2 } },
      );
    });

    it('should publish a delta with codec option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'delta-002',
        channel: 'diffs',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      const result = await client.publishDelta('diffs', 'old', 'new', { codec: MessageCodec.JSON });

      expect(result.id).toBe('delta-002');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/diffs/publish-delta',
        { old_value: 'old', new_value: 'new', codec: 'json' },
      );
    });

    it('should publish a delta with binary codec', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'delta-003',
        channel: 'binary-diffs',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publishDelta('binary-diffs', [0, 1], [1, 0], { codec: MessageCodec.BINARY });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/binary-diffs/publish-delta',
        { old_value: [0, 1], new_value: [1, 0], codec: 'binary' },
      );
    });

    it('should publish a delta with msgpack codec', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'delta-004',
        channel: 'msgpack-diffs',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publishDelta('msgpack-diffs', { a: 1 }, { a: 2 }, { codec: MessageCodec.MSGPACK });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/msgpack-diffs/publish-delta',
        { old_value: { a: 1 }, new_value: { a: 2 }, codec: 'msgpack' },
      );
    });

    it('should URL encode the channel name', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'delta-005',
        channel: 'my channel',
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      await client.publishDelta('my channel', 'a', 'b');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/my%20channel/publish-delta',
        { old_value: 'a', new_value: 'b' },
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.publishDelta('events', 'old', 'new');

      expect(result.id).toBe('');
      expect(result.channel).toBe('events');
      expect(result.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.publishDelta('events', 'old', 'new');

      expect(result.id).toBe('');
      expect(result.channel).toBe('events');
      expect(result.timestamp).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    });

    it('should default channel to channelName when missing in response', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'delta-099' });

      const result = await client.publishDelta('my-channel', 'old', 'new');

      expect(result.channel).toBe('my-channel');
    });

    it('should publish delta with string values', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'delta-str', channel: 'test', timestamp: '2025-01-17T00:00:00.000Z' });

      await client.publishDelta('test', 'before', 'after');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/test/publish-delta',
        { old_value: 'before', new_value: 'after' },
      );
    });

    it('should publish delta with numeric values', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'delta-num', channel: 'test', timestamp: '2025-01-17T00:00:00.000Z' });

      await client.publishDelta('test', 10, 20);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/messaging/channels/test/publish-delta',
        { old_value: 10, new_value: 20 },
      );
    });

    it('should throw ValidationError when channelName is empty', async () => {
      await expect(client.publishDelta('', 'old', 'new')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is null', async () => {
      await expect(client.publishDelta(null, 'old', 'new')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is undefined', async () => {
      await expect(client.publishDelta(undefined, 'old', 'new')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when oldValue is null', async () => {
      await expect(client.publishDelta('events', null, 'new')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when oldValue is undefined', async () => {
      await expect(client.publishDelta('events', undefined, 'new')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newValue is null', async () => {
      await expect(client.publishDelta('events', 'old', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newValue is undefined', async () => {
      await expect(client.publishDelta('events', 'old', undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when codec is invalid', async () => {
      await expect(client.publishDelta('events', 'old', 'new', { codec: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.post.mockRejectedValueOnce(new Error('Server unavailable'));

      await expect(client.publishDelta('events', 'old', 'new')).rejects.toThrow('Server unavailable');
    });
  });

  // ---------------------------------------------------------------------------
  // Subscription operations
  // ---------------------------------------------------------------------------

  describe('subscribe', () => {
    let mockEventSource;
    let originalEventSource;

    beforeEach(() => {
      mockEventSource = {
        onmessage: null,
        onerror: null,
        close: jest.fn(),
      };
      originalEventSource = global.EventSource;
      global.EventSource = jest.fn(() => mockEventSource);
    });

    afterEach(() => {
      global.EventSource = originalEventSource;
    });

    it('should create a subscription for a channel', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });

      expect(subscription).toBeDefined();
      expect(subscription.closed).toBe(false);
      expect(typeof subscription.close).toBe('function');
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/messaging/channels/events/subscribe',
      );
    });

    it('should URL encode the channel name in subscription URL', async () => {
      const onMessage = jest.fn();

      await client.subscribe('my channel', { onMessage });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/messaging/channels/my%20channel/subscribe',
      );
    });

    it('should call onMessage when a message is received', async () => {
      const onMessage = jest.fn();

      await client.subscribe('events', { onMessage });

      const messageData = JSON.stringify({
        id: 'msg-001',
        channel: 'events',
        payload: { action: 'click' },
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      mockEventSource.onmessage({ data: messageData });

      expect(onMessage).toHaveBeenCalledWith({
        id: 'msg-001',
        channel: 'events',
        payload: { action: 'click' },
        timestamp: '2025-01-17T00:00:00.000Z',
      });
    });

    it('should capture subscription_id from message data', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });

      const messageData = JSON.stringify({
        subscription_id: 'sub-abc-123',
        payload: 'test',
      });

      mockEventSource.onmessage({ data: messageData });

      expect(subscription.id).toBe('sub-abc-123');
    });

    it('should have null id before subscription_id is received', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });

      expect(subscription.id).toBeNull();
    });

    it('should call onError when parse error occurs', async () => {
      const onMessage = jest.fn();
      const onError = jest.fn();

      await client.subscribe('events', { onMessage, onError });

      mockEventSource.onmessage({ data: 'invalid-json{{{' });

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
      expect(onMessage).not.toHaveBeenCalled();
    });

    it('should not throw when parse error occurs and onError is not provided', async () => {
      const onMessage = jest.fn();

      await client.subscribe('events', { onMessage });

      mockEventSource.onmessage({ data: 'invalid-json' });

      expect(onMessage).not.toHaveBeenCalled();
    });

    it('should call onError when connection error occurs', async () => {
      const onMessage = jest.fn();
      const onError = jest.fn();

      await client.subscribe('events', { onMessage, onError });

      mockEventSource.onerror(new Error('Connection lost'));

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should not throw when connection error occurs and onError is not provided', async () => {
      const onMessage = jest.fn();

      await client.subscribe('events', { onMessage });

      mockEventSource.onerror(new Error('Connection lost'));
    });

    it('should close the subscription', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });
      expect(subscription.closed).toBe(false);

      subscription.close();

      expect(subscription.closed).toBe(true);
      expect(mockEventSource.close).toHaveBeenCalled();
    });

    it('should not process messages after subscription is closed', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });
      subscription.close();

      mockEventSource.onmessage({
        data: JSON.stringify({ payload: 'should be ignored' }),
      });

      expect(onMessage).not.toHaveBeenCalled();
    });

    it('should not call onError after subscription is closed', async () => {
      const onMessage = jest.fn();
      const onError = jest.fn();

      const subscription = await client.subscribe('events', { onMessage, onError });
      subscription.close();

      mockEventSource.onerror(new Error('Connection failed'));

      expect(onError).not.toHaveBeenCalled();
    });

    it('should handle multiple close calls gracefully', async () => {
      const onMessage = jest.fn();

      const subscription = await client.subscribe('events', { onMessage });

      subscription.close();
      subscription.close();
      subscription.close();

      expect(mockEventSource.close).toHaveBeenCalledTimes(1);
    });

    it('should handle multiple messages in sequence', async () => {
      const onMessage = jest.fn();

      await client.subscribe('events', { onMessage });

      mockEventSource.onmessage({ data: JSON.stringify({ id: '1', payload: 'first' }) });
      mockEventSource.onmessage({ data: JSON.stringify({ id: '2', payload: 'second' }) });
      mockEventSource.onmessage({ data: JSON.stringify({ id: '3', payload: 'third' }) });

      expect(onMessage).toHaveBeenCalledTimes(3);
      expect(onMessage).toHaveBeenNthCalledWith(1, expect.objectContaining({ id: '1', payload: 'first' }));
      expect(onMessage).toHaveBeenNthCalledWith(2, expect.objectContaining({ id: '2', payload: 'second' }));
      expect(onMessage).toHaveBeenNthCalledWith(3, expect.objectContaining({ id: '3', payload: 'third' }));
    });

    it('should throw ValidationError when channelName is empty', async () => {
      await expect(client.subscribe('', { onMessage: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is null', async () => {
      await expect(client.subscribe(null, { onMessage: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when channelName is undefined', async () => {
      await expect(client.subscribe(undefined, { onMessage: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onMessage is missing', async () => {
      await expect(client.subscribe('events', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onMessage is null', async () => {
      await expect(client.subscribe('events', { onMessage: null })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onMessage is not a function', async () => {
      await expect(client.subscribe('events', { onMessage: 'not-a-function' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onError is not a function', async () => {
      await expect(
        client.subscribe('events', { onMessage: jest.fn(), onError: 'not-a-function' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle EventSource constructor failure', async () => {
      global.EventSource = jest.fn(() => {
        throw new Error('EventSource not supported');
      });

      const onMessage = jest.fn();

      await expect(client.subscribe('events', { onMessage })).rejects.toThrow(QuicBaaSError);
    });

    it('should include error code SUBSCRIPTION_ERROR on EventSource failure', async () => {
      global.EventSource = jest.fn(() => {
        throw new Error('EventSource not supported');
      });

      const onMessage = jest.fn();

      try {
        await client.subscribe('events', { onMessage });
      } catch (error) {
        expect(error).toBeInstanceOf(QuicBaaSError);
        expect(error.code).toBe('SUBSCRIPTION_ERROR');
      }
    });

    it('should include error code PARSE_ERROR on invalid JSON', async () => {
      const onMessage = jest.fn();
      const onError = jest.fn();

      await client.subscribe('events', { onMessage, onError });

      mockEventSource.onmessage({ data: '{{invalid' });

      expect(onError).toHaveBeenCalledWith(
        expect.objectContaining({ code: 'PARSE_ERROR' }),
      );
    });

    it('should include error code SUBSCRIPTION_ERROR on connection error', async () => {
      const onMessage = jest.fn();
      const onError = jest.fn();

      await client.subscribe('events', { onMessage, onError });

      mockEventSource.onerror(new Error('Connection reset'));

      expect(onError).toHaveBeenCalledWith(
        expect.objectContaining({ code: 'SUBSCRIPTION_ERROR' }),
      );
    });
  });

  describe('unsubscribe', () => {
    it('should unsubscribe by subscription ID', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.unsubscribe('sub-abc-123');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/messaging/subscriptions/sub-abc-123',
      );
    });

    it('should URL encode the subscription ID', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.unsubscribe('sub/special id');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/messaging/subscriptions/sub%2Fspecial%20id',
      );
    });

    it('should throw ValidationError when subscriptionId is empty', async () => {
      await expect(client.unsubscribe('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when subscriptionId is null', async () => {
      await expect(client.unsubscribe(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when subscriptionId is undefined', async () => {
      await expect(client.unsubscribe(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when subscriptionId is not a string', async () => {
      await expect(client.unsubscribe(123)).rejects.toThrow(ValidationError);
    });

    it('should propagate transport errors', async () => {
      mockTransport.delete.mockRejectedValueOnce(new Error('Subscription not found'));

      await expect(client.unsubscribe('nonexistent')).rejects.toThrow('Subscription not found');
    });
  });

  // ---------------------------------------------------------------------------
  // Watch operations
  // ---------------------------------------------------------------------------

  describe('watchPrefix', () => {
    let mockEventSource;
    let originalEventSource;

    beforeEach(() => {
      mockEventSource = {
        onmessage: null,
        onerror: null,
        close: jest.fn(),
      };
      originalEventSource = global.EventSource;
      global.EventSource = jest.fn(() => mockEventSource);
    });

    afterEach(() => {
      global.EventSource = originalEventSource;
    });

    it('should create a watcher for a prefix', async () => {
      const onEvent = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent });

      expect(subscription).toBeDefined();
      expect(subscription.closed).toBe(false);
      expect(typeof subscription.close).toBe('function');
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/messaging/watch/user%3A',
      );
    });

    it('should URL encode the prefix', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('my prefix', { onEvent });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/messaging/watch/my%20prefix',
      );
    });

    it('should call onEvent when a watch event is received', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('user:', { onEvent });

      const eventData = JSON.stringify({
        type: 'PUT',
        key: 'user:123',
        value: { name: 'Alice' },
        revision: 42,
      });

      mockEventSource.onmessage({ data: eventData });

      expect(onEvent).toHaveBeenCalledWith({
        type: 'PUT',
        key: 'user:123',
        value: { name: 'Alice' },
        revision: 42,
      });
    });

    it('should capture subscription_id from watch event', async () => {
      const onEvent = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent });

      mockEventSource.onmessage({
        data: JSON.stringify({ subscription_id: 'watch-sub-001', type: 'PUT', key: 'user:1' }),
      });

      expect(subscription.id).toBe('watch-sub-001');
    });

    it('should call onError when parse error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      await client.watchPrefix('user:', { onEvent, onError });

      mockEventSource.onmessage({ data: 'not-json' });

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should not throw when parse error occurs and onError is not provided', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('user:', { onEvent });

      mockEventSource.onmessage({ data: '{{{bad json' });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should call onError when connection error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      await client.watchPrefix('user:', { onEvent, onError });

      mockEventSource.onerror(new Error('Stream interrupted'));

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should not throw when connection error occurs and onError is not provided', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('user:', { onEvent });

      mockEventSource.onerror(new Error('Stream interrupted'));
    });

    it('should close the watcher', async () => {
      const onEvent = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent });
      expect(subscription.closed).toBe(false);

      subscription.close();

      expect(subscription.closed).toBe(true);
      expect(mockEventSource.close).toHaveBeenCalled();
    });

    it('should not process events after watcher is closed', async () => {
      const onEvent = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent });
      subscription.close();

      mockEventSource.onmessage({
        data: JSON.stringify({ type: 'PUT', key: 'user:1', value: 'ignored' }),
      });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should not call onError after watcher is closed', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent, onError });
      subscription.close();

      mockEventSource.onerror(new Error('Connection failed'));

      expect(onError).not.toHaveBeenCalled();
    });

    it('should handle multiple close calls gracefully', async () => {
      const onEvent = jest.fn();

      const subscription = await client.watchPrefix('user:', { onEvent });

      subscription.close();
      subscription.close();
      subscription.close();

      expect(mockEventSource.close).toHaveBeenCalledTimes(1);
    });

    it('should handle multiple events in sequence', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('user:', { onEvent });

      mockEventSource.onmessage({ data: JSON.stringify({ type: 'PUT', key: 'user:1' }) });
      mockEventSource.onmessage({ data: JSON.stringify({ type: 'DELETE', key: 'user:2' }) });
      mockEventSource.onmessage({ data: JSON.stringify({ type: 'PUT', key: 'user:3' }) });

      expect(onEvent).toHaveBeenCalledTimes(3);
      expect(onEvent).toHaveBeenNthCalledWith(1, expect.objectContaining({ key: 'user:1' }));
      expect(onEvent).toHaveBeenNthCalledWith(2, expect.objectContaining({ key: 'user:2' }));
      expect(onEvent).toHaveBeenNthCalledWith(3, expect.objectContaining({ key: 'user:3' }));
    });

    it('should throw ValidationError when prefix is empty', async () => {
      await expect(client.watchPrefix('', { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is null', async () => {
      await expect(client.watchPrefix(null, { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is undefined', async () => {
      await expect(client.watchPrefix(undefined, { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is not a string', async () => {
      await expect(client.watchPrefix(123, { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is missing', async () => {
      await expect(client.watchPrefix('user:', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is null', async () => {
      await expect(client.watchPrefix('user:', { onEvent: null })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is not a function', async () => {
      await expect(client.watchPrefix('user:', { onEvent: 'not-a-function' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onError is not a function', async () => {
      await expect(
        client.watchPrefix('user:', { onEvent: jest.fn(), onError: 'not-a-function' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle EventSource constructor failure', async () => {
      global.EventSource = jest.fn(() => {
        throw new Error('EventSource not available');
      });

      const onEvent = jest.fn();

      await expect(client.watchPrefix('user:', { onEvent })).rejects.toThrow(QuicBaaSError);
    });

    it('should include error code SUBSCRIPTION_ERROR on EventSource failure', async () => {
      global.EventSource = jest.fn(() => {
        throw new Error('Not supported');
      });

      const onEvent = jest.fn();

      try {
        await client.watchPrefix('user:', { onEvent });
      } catch (error) {
        expect(error).toBeInstanceOf(QuicBaaSError);
        expect(error.code).toBe('SUBSCRIPTION_ERROR');
      }
    });
  });

  // ---------------------------------------------------------------------------
  // Stats operations
  // ---------------------------------------------------------------------------

  describe('getStats', () => {
    it('should return messaging stats with snake_case response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        total_channels: 10,
        total_subscribers: 50,
        total_messages: 100000,
        messages_per_second: 250,
      });

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 10,
        totalSubscribers: 50,
        totalMessages: 100000,
        messagesPerSecond: 250,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/messaging/stats',
      );
    });

    it('should return messaging stats with camelCase response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        totalChannels: 5,
        totalSubscribers: 20,
        totalMessages: 50000,
        messagesPerSecond: 100,
      });

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 5,
        totalSubscribers: 20,
        totalMessages: 50000,
        messagesPerSecond: 100,
      });
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 0,
        totalSubscribers: 0,
        totalMessages: 0,
        messagesPerSecond: 0,
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 0,
        totalSubscribers: 0,
        totalMessages: 0,
        messagesPerSecond: 0,
      });
    });

    it('should handle partial response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        total_channels: 3,
        total_messages: 500,
      });

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 3,
        totalSubscribers: 0,
        totalMessages: 500,
        messagesPerSecond: 0,
      });
    });

    it('should prefer camelCase fields over snake_case when both exist', async () => {
      mockTransport.get.mockResolvedValueOnce({
        totalChannels: 10,
        total_channels: 5,
        totalSubscribers: 20,
        total_subscribers: 10,
        totalMessages: 1000,
        total_messages: 500,
        messagesPerSecond: 50,
        messages_per_second: 25,
      });

      const result = await client.getStats();

      expect(result).toEqual({
        totalChannels: 10,
        totalSubscribers: 20,
        totalMessages: 1000,
        messagesPerSecond: 50,
      });
    });

    it('should propagate transport errors', async () => {
      mockTransport.get.mockRejectedValueOnce(new Error('Stats unavailable'));

      await expect(client.getStats()).rejects.toThrow('Stats unavailable');
    });
  });

  describe('getDedupStats', () => {
    it('should return dedup stats with snake_case response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        total_keys: 5000,
        duplicates_blocked: 1200,
        hit_rate: 0.85,
      });

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 5000,
        duplicatesBlocked: 1200,
        hitRate: 0.85,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/messaging/dedup-stats',
      );
    });

    it('should return dedup stats with camelCase response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        totalKeys: 3000,
        duplicatesBlocked: 800,
        hitRate: 0.92,
      });

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 3000,
        duplicatesBlocked: 800,
        hitRate: 0.92,
      });
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 0,
        duplicatesBlocked: 0,
        hitRate: 0,
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 0,
        duplicatesBlocked: 0,
        hitRate: 0,
      });
    });

    it('should handle partial response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        total_keys: 100,
      });

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 100,
        duplicatesBlocked: 0,
        hitRate: 0,
      });
    });

    it('should prefer camelCase fields over snake_case when both exist', async () => {
      mockTransport.get.mockResolvedValueOnce({
        totalKeys: 5000,
        total_keys: 2000,
        duplicatesBlocked: 1200,
        duplicates_blocked: 600,
        hitRate: 0.95,
        hit_rate: 0.50,
      });

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 5000,
        duplicatesBlocked: 1200,
        hitRate: 0.95,
      });
    });

    it('should handle zero values in response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        total_keys: 0,
        duplicates_blocked: 0,
        hit_rate: 0,
      });

      const result = await client.getDedupStats();

      expect(result).toEqual({
        totalKeys: 0,
        duplicatesBlocked: 0,
        hitRate: 0,
      });
    });

    it('should propagate transport errors', async () => {
      mockTransport.get.mockRejectedValueOnce(new Error('Dedup stats unavailable'));

      await expect(client.getDedupStats()).rejects.toThrow('Dedup stats unavailable');
    });
  });
});

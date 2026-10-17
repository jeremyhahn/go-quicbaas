/**
 * QuicBaaS Client Tests
 */

import { QuicBaaS } from '../src/quicbaas.js';
import { ValidationError } from '../src/errors/index.js';

describe('QuicBaaS', () => {
  describe('constructor', () => {
    it('should create a client with valid baseUrl', () => {
      const db = new QuicBaaS('https://localhost:8080');
      expect(db).toBeInstanceOf(QuicBaaS);
    });

    it('should throw ValidationError when baseUrl is missing', () => {
      expect(() => new QuicBaaS()).toThrow(ValidationError);
      expect(() => new QuicBaaS('')).toThrow(ValidationError);
      expect(() => new QuicBaaS(null)).toThrow(ValidationError);
    });

    it('should accept custom options', () => {
      const db = new QuicBaaS('https://localhost:8080', {
        timeout: 60000,
        retries: 5,
      });
      expect(db).toBeInstanceOf(QuicBaaS);
    });
  });

  describe('transport', () => {
    it('should expose the transport', () => {
      const db = new QuicBaaS('https://localhost:8080');
      expect(db.transport).toBeDefined();
    });

    it('should allow setting auth token', () => {
      const db = new QuicBaaS('https://localhost:8080');
      expect(() => db.setAuthToken('test-token')).not.toThrow();
    });

    it('should allow clearing auth token', () => {
      const db = new QuicBaaS('https://localhost:8080');
      db.setAuthToken('test-token');
      expect(() => db.clearAuthToken()).not.toThrow();
    });
  });

  describe('client accessors', () => {
    let db;

    beforeEach(() => {
      db = new QuicBaaS('https://localhost:8080');
    });

    // Core Data Operations
    it('should return KV client', () => {
      const kv = db.kv();
      expect(kv).toBeDefined();
      expect(db.kv()).toBe(kv); // Same instance (cached)
    });

    it('should return Batch client', () => {
      const batch = db.batch();
      expect(batch).toBeDefined();
      expect(db.batch()).toBe(batch);
    });

    it('should return Transaction client', () => {
      const txn = db.transaction();
      expect(txn).toBeDefined();
      expect(db.transaction()).toBe(txn);
    });

    it('should return Query client', () => {
      const query = db.query();
      expect(query).toBeDefined();
      expect(db.query()).toBe(query);
    });

    // Cluster Operations
    it('should return Cluster client', () => {
      const cluster = db.cluster();
      expect(cluster).toBeDefined();
      expect(db.cluster()).toBe(cluster);
    });

    it('should return Health client', () => {
      const health = db.health();
      expect(health).toBeDefined();
      expect(db.health()).toBe(health);
    });

    it('should return Operations client', () => {
      const ops = db.operations();
      expect(ops).toBeDefined();
      expect(db.operations()).toBe(ops);
    });

    // Security Operations
    it('should return Auth client', () => {
      const auth = db.auth();
      expect(auth).toBeDefined();
      expect(db.auth()).toBe(auth);
    });

    it('should return Seal client', () => {
      const seal = db.seal();
      expect(seal).toBeDefined();
      expect(db.seal()).toBe(seal);
    });

    // Cryptographic Services
    it('should return KMS client', () => {
      const kms = db.kms();
      expect(kms).toBeDefined();
      expect(db.kms()).toBe(kms);
    });

    it('should return Secrets client', () => {
      const secrets = db.secrets();
      expect(secrets).toBeDefined();
      expect(db.secrets()).toBe(secrets);
    });

    it('should return Crypto client', () => {
      const crypto = db.crypto();
      expect(crypto).toBeDefined();
      expect(db.crypto()).toBe(crypto);
    });

    it('should return DynamicSecrets client', () => {
      const ds = db.dynamicSecrets();
      expect(ds).toBeDefined();
      expect(db.dynamicSecrets()).toBe(ds);
    });

    it('should return Certificate client', () => {
      const cert = db.certificate();
      expect(cert).toBeDefined();
      expect(db.certificate()).toBe(cert);
    });

    // IAM
    it('should return IAM client', () => {
      const iam = db.iam();
      expect(iam).toBeDefined();
      expect(db.iam()).toBe(iam);
    });

    // Specialized Data Operations
    it('should return Vector client', () => {
      const vector = db.vector();
      expect(vector).toBeDefined();
      expect(db.vector()).toBe(vector);
    });

    it('should return TimeSeries client', () => {
      const ts = db.timeSeries();
      expect(ts).toBeDefined();
      expect(db.timeSeries()).toBe(ts);
    });

    it('should return Backup client', () => {
      const backup = db.backup();
      expect(backup).toBeDefined();
      expect(db.backup()).toBe(backup);
    });

    // Storage Operations
    it('should return ObjectStorage client', () => {
      const storage = db.objectStorage();
      expect(storage).toBeDefined();
      expect(db.objectStorage()).toBe(storage);
    });

    it('should return ParameterStore client', () => {
      const params = db.parameterStore();
      expect(params).toBeDefined();
      expect(db.parameterStore()).toBe(params);
    });

    // AI/ML Services
    it('should return Embedding client', () => {
      const embedding = db.embedding();
      expect(embedding).toBeDefined();
      expect(db.embedding()).toBe(embedding);
    });

    it('should return Ollama client', () => {
      const ollama = db.ollama();
      expect(ollama).toBeDefined();
      expect(db.ollama()).toBe(ollama);
    });

    // Cache Management
    it('should return Cache client', () => {
      const cache = db.cache();
      expect(cache).toBeDefined();
      expect(db.cache()).toBe(cache);
    });

    // Scheduler Operations
    it('should return Scheduler client', () => {
      const scheduler = db.scheduler();
      expect(scheduler).toBeDefined();
      expect(db.scheduler()).toBe(scheduler); // Same instance (cached)
    });
  });
});

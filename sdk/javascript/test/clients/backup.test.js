/**
 * BackupClient Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import BackupClient, {
  BackupType,
  BackupStatus,
  RestoreStatus,
} from '../../src/clients/backup.js';
import { ValidationError } from '../../src/errors/index.js';

describe('BackupClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new BackupClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(BackupClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new BackupClient()).toThrow(ValidationError);
      expect(() => new BackupClient(null)).toThrow(ValidationError);
      expect(() => new BackupClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // createBackup Tests
  // ==========================================================================

  describe('createBackup', () => {
    it('should create a backup with required options', async () => {
      const mockResponse = {
        backupId: 'backup-123',
        status: 'PENDING',
        createdAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.createBackup({
        name: 'daily-backup',
        destination: '/backups/daily',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/create', {
        name: 'daily-backup',
        destination: '/backups/daily',
      });
      expect(result.backupId).toBe('backup-123');
      expect(result.status).toBe('PENDING');
    });

    it('should create a backup with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        backupId: 'backup-123',
        status: 'PENDING',
        createdAt: '2025-01-17T00:00:00Z',
      });

      await client.createBackup({
        name: 'full-backup',
        description: 'Weekly full backup',
        type: BackupType.FULL,
        destination: 's3://bucket/backups',
        compress: true,
        encrypt: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/create', {
        name: 'full-backup',
        description: 'Weekly full backup',
        type: 'FULL',
        destination: 's3://bucket/backups',
        compress: true,
        encrypt: true,
      });
    });

    it('should create an incremental backup', async () => {
      mockTransport.post.mockResolvedValueOnce({
        backupId: 'backup-124',
        status: 'PENDING',
        createdAt: '2025-01-17T00:00:00Z',
      });

      await client.createBackup({
        name: 'incremental-backup',
        destination: '/backups/incremental',
        type: BackupType.INCREMENTAL,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/create', {
        name: 'incremental-backup',
        destination: '/backups/incremental',
        type: 'INCREMENTAL',
      });
    });

    it('should handle missing response fields with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.createBackup({
        name: 'backup',
        destination: '/backups',
      });

      expect(result.backupId).toBe('');
      expect(result.status).toBe('PENDING');
      expect(result.createdAt).toBeDefined();
    });

    it('should throw ValidationError when options is missing', async () => {
      await expect(client.createBackup()).rejects.toThrow(ValidationError);
      await expect(client.createBackup(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', async () => {
      await expect(client.createBackup('string')).rejects.toThrow(ValidationError);
      await expect(client.createBackup(['array'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(
        client.createBackup({ destination: '/backups' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(
        client.createBackup({ name: '', destination: '/backups' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when destination is missing', async () => {
      await expect(
        client.createBackup({ name: 'backup' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when destination is empty', async () => {
      await expect(
        client.createBackup({ name: 'backup', destination: '' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when description is not a string', async () => {
      await expect(
        client.createBackup({
          name: 'backup',
          destination: '/backups',
          description: 123,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when type is invalid', async () => {
      await expect(
        client.createBackup({
          name: 'backup',
          destination: '/backups',
          type: 'INVALID',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when compress is not a boolean', async () => {
      await expect(
        client.createBackup({
          name: 'backup',
          destination: '/backups',
          compress: 'yes',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when encrypt is not a boolean', async () => {
      await expect(
        client.createBackup({
          name: 'backup',
          destination: '/backups',
          encrypt: 1,
        }),
      ).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // listBackups Tests
  // ==========================================================================

  describe('listBackups', () => {
    it('should list all backups', async () => {
      const mockResponse = {
        backups: [
          { backupId: 'backup-1', name: 'Backup 1', status: 'COMPLETED' },
          { backupId: 'backup-2', name: 'Backup 2', status: 'IN_PROGRESS' },
        ],
        total: 2,
        hasMore: false,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listBackups();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/list', { query: {} });
      expect(result.backups).toHaveLength(2);
      expect(result.total).toBe(2);
      expect(result.hasMore).toBe(false);
    });

    it('should list backups with pagination', async () => {
      mockTransport.get.mockResolvedValueOnce({
        backups: [{ backupId: 'backup-1' }],
        total: 10,
        hasMore: true,
      });

      await client.listBackups({ limit: 1, offset: 0 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/list', {
        query: { limit: 1, offset: 0 },
      });
    });

    it('should list backups filtered by type', async () => {
      mockTransport.get.mockResolvedValueOnce({ backups: [], total: 0, hasMore: false });

      await client.listBackups({ type: BackupType.FULL });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/list', {
        query: { type: 'FULL' },
      });
    });

    it('should list backups filtered by status', async () => {
      mockTransport.get.mockResolvedValueOnce({ backups: [], total: 0, hasMore: false });

      await client.listBackups({ status: BackupStatus.COMPLETED });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/list', {
        query: { status: 'COMPLETED' },
      });
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.listBackups();

      expect(result.backups).toEqual([]);
      expect(result.total).toBe(0);
      expect(result.hasMore).toBe(false);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listBackups({ limit: 0 })).rejects.toThrow(ValidationError);
      await expect(client.listBackups({ limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(client.listBackups({ offset: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when type is invalid', async () => {
      await expect(client.listBackups({ type: 'INVALID' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is invalid', async () => {
      await expect(client.listBackups({ status: 'INVALID' })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // getBackup Tests
  // ==========================================================================

  describe('getBackup', () => {
    it('should get backup information', async () => {
      const mockResponse = {
        backupId: 'backup-123',
        name: 'Daily Backup',
        description: 'Daily production backup',
        type: 'FULL',
        status: 'COMPLETED',
        destination: '/backups/daily',
        size: 1024000,
        compressed: true,
        encrypted: false,
        createdAt: '2025-01-17T00:00:00Z',
        completedAt: '2025-01-17T01:00:00Z',
        durationMs: 3600000,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getBackup('backup-123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/backup-123');
      expect(result.backupId).toBe('backup-123');
      expect(result.name).toBe('Daily Backup');
      expect(result.size).toBe(1024000);
      expect(result.compressed).toBe(true);
    });

    it('should handle id field in response', async () => {
      mockTransport.get.mockResolvedValueOnce({ id: 'backup-123', name: 'Backup' });

      const result = await client.getBackup('backup-123');

      expect(result.backupId).toBe('backup-123');
    });

    it('should handle compress/encrypt field names', async () => {
      mockTransport.get.mockResolvedValueOnce({
        backupId: 'backup-123',
        compress: true,
        encrypt: true,
      });

      const result = await client.getBackup('backup-123');

      expect(result.compressed).toBe(true);
      expect(result.encrypted).toBe(true);
    });

    it('should handle duration field name', async () => {
      mockTransport.get.mockResolvedValueOnce({
        backupId: 'backup-123',
        duration: 5000,
      });

      const result = await client.getBackup('backup-123');

      expect(result.durationMs).toBe(5000);
    });

    it('should URL-encode backup ID', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'backup/123' });

      await client.getBackup('backup/123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/backup%2F123');
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getBackup('backup-123');

      expect(result.backupId).toBe('');
      expect(result.name).toBe('');
      expect(result.type).toBe('FULL');
      expect(result.status).toBe('PENDING');
    });

    it('should throw ValidationError when backupId is empty', async () => {
      await expect(client.getBackup('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when backupId is not a string', async () => {
      await expect(client.getBackup(123)).rejects.toThrow(ValidationError);
      await expect(client.getBackup(null)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // restoreBackup Tests
  // ==========================================================================

  describe('restoreBackup', () => {
    it('should start backup restore', async () => {
      const mockResponse = {
        restoreId: 'restore-456',
        backupId: 'backup-123',
        status: 'PENDING',
        startedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.restoreBackup('backup-123');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/backup-123/restore', {});
      expect(result.restoreId).toBe('restore-456');
      expect(result.backupId).toBe('backup-123');
      expect(result.status).toBe('PENDING');
    });

    it('should restore with options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        restoreId: 'restore-456',
        status: 'PENDING',
      });

      await client.restoreBackup('backup-123', {
        targetPath: '/data/restore',
        overwrite: true,
        validateFirst: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/backup-123/restore', {
        targetPath: '/data/restore',
        overwrite: true,
        validateFirst: true,
      });
    });

    it('should handle missing response fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.restoreBackup('backup-123');

      expect(result.restoreId).toBe('');
      expect(result.backupId).toBe('backup-123');
      expect(result.status).toBe('PENDING');
    });

    it('should throw ValidationError when backupId is empty', async () => {
      await expect(client.restoreBackup('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when backupId is not a string', async () => {
      await expect(client.restoreBackup(123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targetPath is not a string', async () => {
      await expect(
        client.restoreBackup('backup-123', { targetPath: 123 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when overwrite is not a boolean', async () => {
      await expect(
        client.restoreBackup('backup-123', { overwrite: 'yes' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when validateFirst is not a boolean', async () => {
      await expect(
        client.restoreBackup('backup-123', { validateFirst: 1 }),
      ).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // deleteBackup Tests
  // ==========================================================================

  describe('deleteBackup', () => {
    it('should delete a backup', async () => {
      const mockResponse = {
        backupId: 'backup-123',
        deleted: true,
        deletedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.delete.mockResolvedValueOnce(mockResponse);

      const result = await client.deleteBackup('backup-123');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/backup/backup-123');
      expect(result.backupId).toBe('backup-123');
      expect(result.deleted).toBe(true);
    });

    it('should handle missing response fields', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteBackup('backup-123');

      expect(result.backupId).toBe('backup-123');
      expect(result.deleted).toBe(true);
      expect(result.deletedAt).toBeDefined();
    });

    it('should URL-encode backup ID', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true });

      await client.deleteBackup('backup/123');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/backup/backup%2F123');
    });

    it('should throw ValidationError when backupId is empty', async () => {
      await expect(client.deleteBackup('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when backupId is not a string', async () => {
      await expect(client.deleteBackup(null)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // verifyBackup Tests
  // ==========================================================================

  describe('verifyBackup', () => {
    it('should verify backup integrity', async () => {
      const mockResponse = {
        backupId: 'backup-123',
        valid: true,
        checksum: 'sha256:abc123',
        verifiedAt: '2025-01-17T00:00:00Z',
        issues: [],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.verifyBackup('backup-123');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/backup/backup-123/verify', {});
      expect(result.backupId).toBe('backup-123');
      expect(result.valid).toBe(true);
      expect(result.checksum).toBe('sha256:abc123');
    });

    it('should handle invalid backup', async () => {
      const mockResponse = {
        backupId: 'backup-123',
        valid: false,
        verifiedAt: '2025-01-17T00:00:00Z',
        issues: ['Checksum mismatch', 'Missing files'],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.verifyBackup('backup-123');

      expect(result.valid).toBe(false);
      expect(result.issues).toEqual(['Checksum mismatch', 'Missing files']);
    });

    it('should handle missing response fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.verifyBackup('backup-123');

      expect(result.backupId).toBe('backup-123');
      expect(result.valid).toBe(false);
      expect(result.checksum).toBeNull();
      expect(result.issues).toEqual([]);
    });

    it('should throw ValidationError when backupId is empty', async () => {
      await expect(client.verifyBackup('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when backupId is not a string', async () => {
      await expect(client.verifyBackup(123)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // getRestoreProgress Tests
  // ==========================================================================

  describe('getRestoreProgress', () => {
    it('should get restore progress', async () => {
      const mockResponse = {
        restoreId: 'restore-456',
        backupId: 'backup-123',
        status: 'IN_PROGRESS',
        progress: 45,
        bytesRestored: 450000,
        totalBytes: 1000000,
        startedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRestoreProgress('restore-456');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/restore/restore-456');
      expect(result.restoreId).toBe('restore-456');
      expect(result.status).toBe('IN_PROGRESS');
      expect(result.progress).toBe(45);
      expect(result.bytesRestored).toBe(450000);
    });

    it('should handle completed restore', async () => {
      const mockResponse = {
        restoreId: 'restore-456',
        backupId: 'backup-123',
        status: 'COMPLETED',
        progress: 100,
        bytesRestored: 1000000,
        totalBytes: 1000000,
        startedAt: '2025-01-17T00:00:00Z',
        completedAt: '2025-01-17T01:00:00Z',
        durationMs: 3600000,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRestoreProgress('restore-456');

      expect(result.status).toBe('COMPLETED');
      expect(result.progress).toBe(100);
      expect(result.completedAt).toBe('2025-01-17T01:00:00Z');
      expect(result.durationMs).toBe(3600000);
    });

    it('should handle failed restore', async () => {
      const mockResponse = {
        restoreId: 'restore-456',
        status: 'FAILED',
        progress: 30,
        error: 'Disk full',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRestoreProgress('restore-456');

      expect(result.status).toBe('FAILED');
      expect(result.error).toBe('Disk full');
    });

    it('should handle missing response fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getRestoreProgress('restore-456');

      expect(result.restoreId).toBe('restore-456');
      expect(result.backupId).toBe('');
      expect(result.status).toBe('PENDING');
      expect(result.progress).toBe(0);
      expect(result.bytesRestored).toBe(0);
      expect(result.totalBytes).toBe(0);
    });

    it('should URL-encode restore ID', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getRestoreProgress('restore/456');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/backup/restore/restore%2F456');
    });

    it('should throw ValidationError when restoreId is empty', async () => {
      await expect(client.getRestoreProgress('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when restoreId is not a string', async () => {
      await expect(client.getRestoreProgress(456)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Status Parsing Tests
  // ==========================================================================

  describe('status parsing', () => {
    it('should parse numeric backup status', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 0 });
      let result = await client.getBackup('b1');
      expect(result.status).toBe('PENDING');

      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 1 });
      result = await client.getBackup('b1');
      expect(result.status).toBe('IN_PROGRESS');

      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 2 });
      result = await client.getBackup('b1');
      expect(result.status).toBe('COMPLETED');

      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 3 });
      result = await client.getBackup('b1');
      expect(result.status).toBe('FAILED');
    });

    it('should parse lowercase backup status', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 'completed' });

      const result = await client.getBackup('b1');

      expect(result.status).toBe('COMPLETED');
    });

    it('should parse numeric restore status', async () => {
      mockTransport.get.mockResolvedValueOnce({ restoreId: 'r1', status: 0 });
      let result = await client.getRestoreProgress('r1');
      expect(result.status).toBe('PENDING');

      mockTransport.get.mockResolvedValueOnce({ restoreId: 'r1', status: 2 });
      result = await client.getRestoreProgress('r1');
      expect(result.status).toBe('COMPLETED');
    });

    it('should parse lowercase restore status', async () => {
      mockTransport.get.mockResolvedValueOnce({ restoreId: 'r1', status: 'in_progress' });

      const result = await client.getRestoreProgress('r1');

      expect(result.status).toBe('IN_PROGRESS');
    });

    it('should parse numeric backup type', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', type: 0 });
      let result = await client.getBackup('b1');
      expect(result.type).toBe('FULL');

      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', type: 1 });
      result = await client.getBackup('b1');
      expect(result.type).toBe('INCREMENTAL');
    });

    it('should parse lowercase backup type', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', type: 'incremental' });

      const result = await client.getBackup('b1');

      expect(result.type).toBe('INCREMENTAL');
    });

    it('should default to FULL for unknown type', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', type: 999 });

      const result = await client.getBackup('b1');

      expect(result.type).toBe('FULL');
    });

    it('should default to PENDING for unknown status', async () => {
      mockTransport.get.mockResolvedValueOnce({ backupId: 'b1', status: 999 });

      const result = await client.getBackup('b1');

      expect(result.status).toBe('PENDING');
    });
  });

  // ==========================================================================
  // Enum Exports Tests
  // ==========================================================================

  describe('BackupType enum', () => {
    it('should export FULL', () => {
      expect(BackupType.FULL).toBe('FULL');
    });

    it('should export INCREMENTAL', () => {
      expect(BackupType.INCREMENTAL).toBe('INCREMENTAL');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(BackupType)).toBe(true);
    });
  });

  describe('BackupStatus enum', () => {
    it('should export all status values', () => {
      expect(BackupStatus.PENDING).toBe('PENDING');
      expect(BackupStatus.IN_PROGRESS).toBe('IN_PROGRESS');
      expect(BackupStatus.COMPLETED).toBe('COMPLETED');
      expect(BackupStatus.FAILED).toBe('FAILED');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(BackupStatus)).toBe(true);
    });
  });

  describe('RestoreStatus enum', () => {
    it('should export all status values', () => {
      expect(RestoreStatus.PENDING).toBe('PENDING');
      expect(RestoreStatus.IN_PROGRESS).toBe('IN_PROGRESS');
      expect(RestoreStatus.COMPLETED).toBe('COMPLETED');
      expect(RestoreStatus.FAILED).toBe('FAILED');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(RestoreStatus)).toBe(true);
    });
  });
});

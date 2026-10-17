/**
 * CertificateClient Tests
 *
 * Comprehensive tests for the QuicBaaS Certificate Client covering
 * certificate issuance, CSR signing, revocation, verification, and TLS configuration.
 *
 * @module test/clients/certificate
 */

import CertificateClient, {
  CertificateUsage,
  KeyAlgorithm,
  TLSVersion,
} from '../../src/clients/certificate.js';
import { ValidationError } from '../../src/errors/index.js';

describe('CertificateClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      request: jest.fn(),
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new CertificateClient(mockTransport);
  });

  // ===========================================================================
  // Constructor Tests
  // ===========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const transport = { request: jest.fn() };
      const certClient = new CertificateClient(transport);
      expect(certClient).toBeInstanceOf(CertificateClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new CertificateClient()).toThrow(ValidationError);
      expect(() => new CertificateClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new CertificateClient(null)).toThrow(ValidationError);
      expect(() => new CertificateClient(null)).toThrow('transport is required');
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new CertificateClient({})).toThrow(ValidationError);
      expect(() => new CertificateClient({})).toThrow('transport must have a request method');
    });

    it('should throw ValidationError when transport.request is not a function', () => {
      expect(() => new CertificateClient({ request: 'not-a-function' })).toThrow(ValidationError);
    });
  });

  // ===========================================================================
  // Enums Tests
  // ===========================================================================

  describe('CertificateUsage enum', () => {
    it('should have correct values', () => {
      expect(CertificateUsage.CLIENT).toBe('client');
      expect(CertificateUsage.SERVER).toBe('server');
      expect(CertificateUsage.BOTH).toBe('both');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(CertificateUsage)).toBe(true);
    });
  });

  describe('KeyAlgorithm enum', () => {
    it('should have correct values', () => {
      expect(KeyAlgorithm.ECDSA).toBe('ecdsa');
      expect(KeyAlgorithm.RSA).toBe('rsa');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(KeyAlgorithm)).toBe(true);
    });
  });

  describe('TLSVersion enum', () => {
    it('should have correct values', () => {
      expect(TLSVersion.TLS_1_2).toBe('1.2');
      expect(TLSVersion.TLS_1_3).toBe('1.3');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(TLSVersion)).toBe(true);
    });
  });

  // ===========================================================================
  // issueCertificate Tests
  // ===========================================================================

  describe('issueCertificate', () => {
    const validRequest = {
      commonName: 'test.example.com',
    };

    const mockResponse = {
      certificate_pem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
      private_key_pem: '-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----',
      ca_certificate_pem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
      chain_pem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
      serial_number: '1234567890abcdef',
      not_before: '2025-01-01T00:00:00Z',
      not_after: '2026-01-01T00:00:00Z',
    };

    it('should issue a certificate with minimal request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.issueCertificate(validRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/issue',
        { common_name: 'test.example.com' },
      );
      expect(result.certificatePem).toBe(mockResponse.certificate_pem);
      expect(result.privateKeyPem).toBe(mockResponse.private_key_pem);
      expect(result.caCertificatePem).toBe(mockResponse.ca_certificate_pem);
      expect(result.chainPem).toBe(mockResponse.chain_pem);
      expect(result.serialNumber).toBe(mockResponse.serial_number);
      expect(result.notBefore).toBe(mockResponse.not_before);
      expect(result.notAfter).toBe(mockResponse.not_after);
    });

    it('should issue a certificate with all options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const fullRequest = {
        commonName: 'api.example.com',
        organization: 'Example Inc',
        organizationalUnit: 'Engineering',
        country: 'US',
        province: 'California',
        locality: 'San Francisco',
        dnsNames: ['api.example.com', 'api2.example.com'],
        ipAddresses: ['10.0.0.1', '10.0.0.2'],
        emailAddresses: ['admin@example.com'],
        uris: ['https://example.com'],
        validityDays: 365,
        usage: CertificateUsage.SERVER,
        isCA: false,
        maxPathLen: 0,
        keyAlgorithm: KeyAlgorithm.ECDSA,
        keySize: 256,
        metadata: { environment: 'production' },
      };

      await client.issueCertificate(fullRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/issue',
        expect.objectContaining({
          common_name: 'api.example.com',
          organization: 'Example Inc',
          organizational_unit: 'Engineering',
          country: 'US',
          province: 'California',
          locality: 'San Francisco',
          dns_names: ['api.example.com', 'api2.example.com'],
          ip_addresses: ['10.0.0.1', '10.0.0.2'],
          email_addresses: ['admin@example.com'],
          uris: ['https://example.com'],
          validity_days: 365,
          usage: 'server',
          is_ca: false,
          max_path_len: 0,
          key_algorithm: 'ecdsa',
          key_size: 256,
          metadata: { environment: 'production' },
        }),
      );
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.issueCertificate()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.issueCertificate(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when commonName is missing', async () => {
      await expect(client.issueCertificate({})).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate({})).rejects.toThrow(/commonName/);
    });

    it('should throw ValidationError when commonName is empty', async () => {
      await expect(client.issueCertificate({ commonName: '' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when commonName is not a string', async () => {
      await expect(client.issueCertificate({ commonName: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid usage value', async () => {
      const request = { commonName: 'test.example.com', usage: 'invalid' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid keyAlgorithm value', async () => {
      const request = { commonName: 'test.example.com', keyAlgorithm: 'invalid' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative validityDays', async () => {
      const request = { commonName: 'test.example.com', validityDays: -1 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for zero validityDays', async () => {
      const request = { commonName: 'test.example.com', validityDays: 0 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/greater than 0/);
    });

    it('should throw ValidationError for non-integer validityDays', async () => {
      const request = { commonName: 'test.example.com', validityDays: 1.5 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative maxPathLen', async () => {
      const request = { commonName: 'test.example.com', maxPathLen: -1 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for zero keySize', async () => {
      const request = { commonName: 'test.example.com', keySize: 0 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/greater than 0/);
    });

    it('should throw ValidationError for non-integer keySize', async () => {
      const request = { commonName: 'test.example.com', keySize: 256.5 };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when dnsNames is not an array', async () => {
      const request = { commonName: 'test.example.com', dnsNames: 'not-an-array' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/must be an array/);
    });

    it('should throw ValidationError when dnsNames contains non-string', async () => {
      const request = { commonName: 'test.example.com', dnsNames: ['valid.com', 123] };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/must be a string/);
    });

    it('should throw ValidationError when ipAddresses is not an array', async () => {
      const request = { commonName: 'test.example.com', ipAddresses: '10.0.0.1' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when emailAddresses is not an array', async () => {
      const request = { commonName: 'test.example.com', emailAddresses: 'email@test.com' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when uris is not an array', async () => {
      const request = { commonName: 'test.example.com', uris: 'https://example.com' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      const request = { commonName: 'test.example.com', metadata: 'not-an-object' };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/must be an object/);
    });

    it('should throw ValidationError when metadata is an array', async () => {
      const request = { commonName: 'test.example.com', metadata: ['key', 'value'] };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata has empty key', async () => {
      const request = { commonName: 'test.example.com', metadata: { '': 'value' } };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/non-empty strings/);
    });

    it('should throw ValidationError when metadata has non-string value', async () => {
      const request = { commonName: 'test.example.com', metadata: { key: 123 } };
      await expect(client.issueCertificate(request)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(request)).rejects.toThrow(/must be a string/);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.issueCertificate(validRequest)).rejects.toThrow(ValidationError);
      await expect(client.issueCertificate(validRequest)).rejects.toThrow(/Invalid issue certificate response/);
    });

    it('should handle empty arrays by not including them in request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const request = {
        commonName: 'test.example.com',
        dnsNames: [],
        ipAddresses: [],
        emailAddresses: [],
        uris: [],
      };

      await client.issueCertificate(request);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/issue',
        { common_name: 'test.example.com' },
      );
    });

    it('should handle empty metadata by not including it in request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const request = {
        commonName: 'test.example.com',
        metadata: {},
      };

      await client.issueCertificate(request);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/issue',
        { common_name: 'test.example.com' },
      );
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        certificatePem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
        privateKeyPem: '-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----',
        caCertificatePem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
        chainPem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
        serialNumber: 'abcdef1234567890',
        notBefore: '2025-01-01T00:00:00Z',
        notAfter: '2026-01-01T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.issueCertificate(validRequest);

      expect(result.certificatePem).toBe(camelCaseResponse.certificatePem);
      expect(result.serialNumber).toBe(camelCaseResponse.serialNumber);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.issueCertificate(validRequest)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // signCSR Tests
  // ===========================================================================

  describe('signCSR', () => {
    const validCSR = '-----BEGIN CERTIFICATE REQUEST-----\nMIIB...\n-----END CERTIFICATE REQUEST-----';

    const mockResponse = {
      certificate_pem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
      ca_certificate_pem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
      chain_pem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
      serial_number: '1234567890abcdef',
      not_before: '2025-01-01T00:00:00Z',
      not_after: '2026-01-01T00:00:00Z',
    };

    it('should sign CSR with default options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.signCSR(validCSR);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/sign',
        { csr_pem: validCSR },
      );
      expect(result.certificatePem).toBe(mockResponse.certificate_pem);
      expect(result.caCertificatePem).toBe(mockResponse.ca_certificate_pem);
      expect(result.chainPem).toBe(mockResponse.chain_pem);
      expect(result.serialNumber).toBe(mockResponse.serial_number);
      expect(result.notBefore).toBe(mockResponse.not_before);
      expect(result.notAfter).toBe(mockResponse.not_after);
    });

    it('should sign CSR with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const options = {
        validityDays: 180,
        usage: CertificateUsage.CLIENT,
      };

      await client.signCSR(validCSR, options);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/sign',
        {
          csr_pem: validCSR,
          validity_days: 180,
          usage: 'client',
        },
      );
    });

    it('should throw ValidationError when csrPem is missing', async () => {
      await expect(client.signCSR()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when csrPem is null', async () => {
      await expect(client.signCSR(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when csrPem is empty', async () => {
      await expect(client.signCSR('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when csrPem is not a string', async () => {
      await expect(client.signCSR(123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid usage option', async () => {
      await expect(client.signCSR(validCSR, { usage: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative validityDays option', async () => {
      await expect(client.signCSR(validCSR, { validityDays: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for zero validityDays option', async () => {
      await expect(client.signCSR(validCSR, { validityDays: 0 })).rejects.toThrow(ValidationError);
      await expect(client.signCSR(validCSR, { validityDays: 0 })).rejects.toThrow(/greater than 0/);
    });

    it('should throw ValidationError for non-integer validityDays option', async () => {
      await expect(client.signCSR(validCSR, { validityDays: 1.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.signCSR(validCSR)).rejects.toThrow(ValidationError);
      await expect(client.signCSR(validCSR)).rejects.toThrow(/Invalid sign CSR response/);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        certificatePem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
        caCertificatePem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
        chainPem: '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----',
        serialNumber: 'abcdef1234567890',
        notBefore: '2025-01-01T00:00:00Z',
        notAfter: '2026-01-01T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.signCSR(validCSR);

      expect(result.certificatePem).toBe(camelCaseResponse.certificatePem);
      expect(result.serialNumber).toBe(camelCaseResponse.serialNumber);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.signCSR(validCSR)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // revokeCertificate Tests
  // ===========================================================================

  describe('revokeCertificate', () => {
    const validSerialNumber = '1234567890abcdef';

    it('should revoke certificate successfully', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const result = await client.revokeCertificate(validSerialNumber);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/revoke',
        { serial_number: validSerialNumber },
      );
      expect(result.success).toBe(true);
    });

    it('should handle revocation failure response', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: false });

      const result = await client.revokeCertificate(validSerialNumber);

      expect(result.success).toBe(false);
    });

    it('should throw ValidationError when serialNumber is missing', async () => {
      await expect(client.revokeCertificate()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when serialNumber is null', async () => {
      await expect(client.revokeCertificate(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when serialNumber is empty', async () => {
      await expect(client.revokeCertificate('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when serialNumber is not a string', async () => {
      await expect(client.revokeCertificate(12345)).rejects.toThrow(ValidationError);
    });

    it('should handle null response with default success false', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.revokeCertificate(validSerialNumber);

      expect(result.success).toBe(false);
    });

    it('should handle undefined success field with default false', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.revokeCertificate(validSerialNumber);

      expect(result.success).toBe(false);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.revokeCertificate(validSerialNumber)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // verifyCertificate Tests
  // ===========================================================================

  describe('verifyCertificate', () => {
    const validCertPem = '-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----';

    it('should verify valid certificate', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: true });

      const result = await client.verifyCertificate(validCertPem);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/verify',
        { certificate_pem: validCertPem },
      );
      expect(result.valid).toBe(true);
      expect(result.errorMessage).toBeUndefined();
    });

    it('should return error message for invalid certificate', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: false,
        error_message: 'Certificate has expired',
      });

      const result = await client.verifyCertificate(validCertPem);

      expect(result.valid).toBe(false);
      expect(result.errorMessage).toBe('Certificate has expired');
    });

    it('should handle camelCase errorMessage response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: false,
        errorMessage: 'Certificate revoked',
      });

      const result = await client.verifyCertificate(validCertPem);

      expect(result.valid).toBe(false);
      expect(result.errorMessage).toBe('Certificate revoked');
    });

    it('should throw ValidationError when certificatePem is missing', async () => {
      await expect(client.verifyCertificate()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when certificatePem is null', async () => {
      await expect(client.verifyCertificate(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when certificatePem is empty', async () => {
      await expect(client.verifyCertificate('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when certificatePem is not a string', async () => {
      await expect(client.verifyCertificate(12345)).rejects.toThrow(ValidationError);
    });

    it('should handle null response with default valid false', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.verifyCertificate(validCertPem);

      expect(result.valid).toBe(false);
    });

    it('should handle undefined valid field with default false', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.verifyCertificate(validCertPem);

      expect(result.valid).toBe(false);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.verifyCertificate(validCertPem)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // getCACertificate Tests
  // ===========================================================================

  describe('getCACertificate', () => {
    const mockResponse = {
      ca_certificate_pem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
      ca_bundle_pem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
      identity: 'QuicBaaS CA',
    };

    it('should get CA certificate successfully', async () => {
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getCACertificate();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/certificate/ca');
      expect(result.caCertificatePem).toBe(mockResponse.ca_certificate_pem);
      expect(result.caBundlePem).toBe(mockResponse.ca_bundle_pem);
      expect(result.identity).toBe(mockResponse.identity);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        caCertificatePem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
        caBundlePem: '-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----',
        identity: 'QuicBaaS CA',
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.getCACertificate();

      expect(result.caCertificatePem).toBe(camelCaseResponse.caCertificatePem);
      expect(result.caBundlePem).toBe(camelCaseResponse.caBundlePem);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getCACertificate()).rejects.toThrow(ValidationError);
      await expect(client.getCACertificate()).rejects.toThrow(/Invalid CA certificate response/);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getCACertificate()).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // getTLSConfig Tests
  // ===========================================================================

  describe('getTLSConfig', () => {
    const mockResponse = {
      server_name: 'api.example.com',
      require_client_cert: true,
      is_server: true,
      min_version: '1.3',
    };

    it('should get TLS config with default options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.getTLSConfig();

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/tls-config',
        {},
      );
      expect(result.serverName).toBe(mockResponse.server_name);
      expect(result.requireClientCert).toBe(mockResponse.require_client_cert);
      expect(result.isServer).toBe(mockResponse.is_server);
      expect(result.minVersion).toBe(mockResponse.min_version);
    });

    it('should get TLS config with all options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const options = {
        serverName: 'api.example.com',
        requireClientCert: true,
        isServer: true,
        minVersion: TLSVersion.TLS_1_3,
      };

      await client.getTLSConfig(options);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/tls-config',
        {
          server_name: 'api.example.com',
          require_client_cert: true,
          is_server: true,
          min_version: '1.3',
        },
      );
    });

    it('should handle partial options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      await client.getTLSConfig({ serverName: 'test.example.com' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/certificate/tls-config',
        { server_name: 'test.example.com' },
      );
    });

    it('should throw ValidationError when serverName is not a string', async () => {
      await expect(client.getTLSConfig({ serverName: 123 })).rejects.toThrow(ValidationError);
      await expect(client.getTLSConfig({ serverName: 123 })).rejects.toThrow(/serverName must be a string/);
    });

    it('should throw ValidationError when requireClientCert is not a boolean', async () => {
      await expect(client.getTLSConfig({ requireClientCert: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.getTLSConfig({ requireClientCert: 'true' })).rejects.toThrow(/requireClientCert must be a boolean/);
    });

    it('should throw ValidationError when isServer is not a boolean', async () => {
      await expect(client.getTLSConfig({ isServer: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.getTLSConfig({ isServer: 'true' })).rejects.toThrow(/isServer must be a boolean/);
    });

    it('should throw ValidationError for invalid minVersion', async () => {
      await expect(client.getTLSConfig({ minVersion: '1.1' })).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.getTLSConfig();

      expect(result.serverName).toBeNull();
      expect(result.requireClientCert).toBe(false);
      expect(result.isServer).toBe(false);
      expect(result.minVersion).toBe(TLSVersion.TLS_1_2);
    });

    it('should handle empty response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.getTLSConfig();

      expect(result.serverName).toBeNull();
      expect(result.requireClientCert).toBe(false);
      expect(result.isServer).toBe(false);
      expect(result.minVersion).toBe(TLSVersion.TLS_1_2);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        serverName: 'api.example.com',
        requireClientCert: true,
        isServer: true,
        minVersion: '1.3',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.getTLSConfig();

      expect(result.serverName).toBe(camelCaseResponse.serverName);
      expect(result.requireClientCert).toBe(camelCaseResponse.requireClientCert);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.getTLSConfig()).rejects.toThrow('Network error');
    });
  });
});

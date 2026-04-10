/**
 * QuicBaaS JavaScript SDK - Certificate Client
 *
 * Provides certificate management operations including certificate issuance,
 * CSR signing, revocation, verification, and TLS configuration.
 *
 * @module @quicbaas/client/clients/certificate
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonNegativeInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for certificate endpoints.
 * @constant {string}
 */
const CERTIFICATE_API_PATH = '/api/v1/certificate';

/**
 * Certificate usage enumeration.
 * Defines the intended use of the certificate for key usage extensions.
 * @readonly
 * @enum {string}
 */
export const CertificateUsage = Object.freeze({
  /** Certificate is for client authentication */
  CLIENT: 'client',
  /** Certificate is for server authentication */
  SERVER: 'server',
  /** Certificate is for both client and server authentication */
  BOTH: 'both',
});

/**
 * Key algorithm enumeration.
 * Defines the cryptographic algorithm used for key generation.
 * @readonly
 * @enum {string}
 */
export const KeyAlgorithm = Object.freeze({
  /** Elliptic Curve Digital Signature Algorithm */
  ECDSA: 'ecdsa',
  /** RSA algorithm */
  RSA: 'rsa',
});

/**
 * TLS version enumeration.
 * Defines the minimum TLS version for TLS configuration.
 * @readonly
 * @enum {string}
 */
export const TLSVersion = Object.freeze({
  /** TLS 1.2 */
  TLS_1_2: '1.2',
  /** TLS 1.3 */
  TLS_1_3: '1.3',
});

/**
 * @typedef {Object} IssueCertificateRequest
 * @property {string} commonName - Common name for the certificate subject (required)
 * @property {string} [organization] - Organization name
 * @property {string} [organizationalUnit] - Organizational unit name
 * @property {string} [country] - Country code (2 letters)
 * @property {string} [province] - Province or state
 * @property {string} [locality] - City or locality
 * @property {string[]} [dnsNames] - DNS subject alternative names
 * @property {string[]} [ipAddresses] - IP address subject alternative names
 * @property {string[]} [emailAddresses] - Email subject alternative names
 * @property {string[]} [uris] - URI subject alternative names
 * @property {number} [validityDays] - Certificate validity in days (default: 365)
 * @property {string} [usage] - Certificate usage: 'client', 'server', or 'both'
 * @property {boolean} [isCA] - Whether this is a CA certificate
 * @property {number} [maxPathLen] - Maximum CA path length
 * @property {string} [keyAlgorithm] - Key algorithm: 'ecdsa' or 'rsa'
 * @property {number} [keySize] - Key size in bits
 * @property {Object<string, string>} [metadata] - Custom metadata key-value pairs
 */

/**
 * @typedef {Object} IssueCertificateResponse
 * @property {string} certificatePem - PEM-encoded certificate
 * @property {string} privateKeyPem - PEM-encoded private key
 * @property {string} caCertificatePem - PEM-encoded CA certificate
 * @property {string} chainPem - PEM-encoded certificate chain
 * @property {string} serialNumber - Certificate serial number
 * @property {string} notBefore - ISO timestamp when certificate becomes valid
 * @property {string} notAfter - ISO timestamp when certificate expires
 */

/**
 * @typedef {Object} SignCSROptions
 * @property {number} [validityDays] - Certificate validity in days
 * @property {string} [usage] - Certificate usage: 'client', 'server', or 'both'
 */

/**
 * @typedef {Object} SignCSRResponse
 * @property {string} certificatePem - PEM-encoded signed certificate
 * @property {string} caCertificatePem - PEM-encoded CA certificate
 * @property {string} chainPem - PEM-encoded certificate chain
 * @property {string} serialNumber - Certificate serial number
 * @property {string} notBefore - ISO timestamp when certificate becomes valid
 * @property {string} notAfter - ISO timestamp when certificate expires
 */

/**
 * @typedef {Object} RevokeCertificateResponse
 * @property {boolean} success - Whether the revocation was successful
 */

/**
 * @typedef {Object} VerifyCertificateResponse
 * @property {boolean} valid - Whether the certificate is valid
 * @property {string} [errorMessage] - Error message if not valid
 */

/**
 * @typedef {Object} GetCACertificateResponse
 * @property {string} caCertificatePem - PEM-encoded CA certificate
 * @property {string} caBundlePem - PEM-encoded CA bundle
 * @property {string} identity - CA identity
 */

/**
 * @typedef {Object} GetTLSConfigOptions
 * @property {string} [serverName] - Server name for SNI
 * @property {boolean} [requireClientCert] - Whether to require client certificates
 * @property {boolean} [isServer] - Whether this is for a server configuration
 * @property {string} [minVersion] - Minimum TLS version: '1.2' or '1.3'
 */

/**
 * @typedef {Object} GetTLSConfigResponse
 * @property {string} serverName - Server name for SNI
 * @property {boolean} requireClientCert - Whether client certificates are required
 * @property {boolean} isServer - Whether this is a server configuration
 * @property {string} minVersion - Minimum TLS version
 */

/**
 * Certificate client for QuicBaaS.
 *
 * Provides operations for certificate lifecycle management including
 * issuance, CSR signing, revocation, verification, and TLS configuration.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import CertificateClient, { CertificateUsage, KeyAlgorithm } from '@quicbaas/client/clients/certificate';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const certificate = new CertificateClient(transport);
 *
 * // Issue a server certificate
 * const cert = await certificate.issueCertificate({
 *   commonName: 'api.example.com',
 *   organization: 'Example Inc',
 *   dnsNames: ['api.example.com', 'api2.example.com'],
 *   validityDays: 365,
 *   usage: CertificateUsage.SERVER,
 *   keyAlgorithm: KeyAlgorithm.ECDSA
 * });
 *
 * console.log('Certificate:', cert.certificatePem);
 * console.log('Expires:', cert.notAfter);
 */
class CertificateClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new CertificateClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided or invalid
   */
  constructor(transport) {
    if (!transport) {
      throw new ValidationError('transport is required', 'transport');
    }
    if (typeof transport.request !== 'function') {
      throw new ValidationError('transport must have a request method', 'transport');
    }

    this.#transport = transport;
  }

  /**
   * Issue a new certificate.
   *
   * Generates a new certificate with the specified parameters.
   * The certificate is signed by the QuicBaaS CA and includes
   * the private key and certificate chain.
   *
   * @param {IssueCertificateRequest} request - Certificate issuance parameters
   * @returns {Promise<IssueCertificateResponse>} The issued certificate with private key
   * @throws {ValidationError} If request parameters are invalid
   * @throws {AuthorizationError} If not authorized to issue certificates
   *
   * @example
   * // Issue a server certificate with ECDSA key
   * const cert = await certificate.issueCertificate({
   *   commonName: 'api.example.com',
   *   organization: 'Example Inc',
   *   dnsNames: ['api.example.com', 'www.example.com'],
   *   ipAddresses: ['10.0.0.1'],
   *   validityDays: 365,
   *   usage: CertificateUsage.SERVER,
   *   keyAlgorithm: KeyAlgorithm.ECDSA
   * });
   *
   * @example
   * // Issue a client certificate
   * const clientCert = await certificate.issueCertificate({
   *   commonName: 'user@example.com',
   *   emailAddresses: ['user@example.com'],
   *   validityDays: 90,
   *   usage: CertificateUsage.CLIENT
   * });
   */
  async issueCertificate(request) {
    this.#validateIssueCertificateRequest(request);

    const body = {
      common_name: request.commonName,
    };

    if (request.organization !== undefined) {
      body.organization = request.organization;
    }

    if (request.organizationalUnit !== undefined) {
      body.organizational_unit = request.organizationalUnit;
    }

    if (request.country !== undefined) {
      body.country = request.country;
    }

    if (request.province !== undefined) {
      body.province = request.province;
    }

    if (request.locality !== undefined) {
      body.locality = request.locality;
    }

    if (request.dnsNames !== undefined && request.dnsNames.length > 0) {
      body.dns_names = request.dnsNames;
    }

    if (request.ipAddresses !== undefined && request.ipAddresses.length > 0) {
      body.ip_addresses = request.ipAddresses;
    }

    if (request.emailAddresses !== undefined && request.emailAddresses.length > 0) {
      body.email_addresses = request.emailAddresses;
    }

    if (request.uris !== undefined && request.uris.length > 0) {
      body.uris = request.uris;
    }

    if (request.validityDays !== undefined) {
      body.validity_days = request.validityDays;
    }

    if (request.usage !== undefined) {
      body.usage = request.usage;
    }

    if (request.isCA !== undefined) {
      body.is_ca = request.isCA;
    }

    if (request.maxPathLen !== undefined) {
      body.max_path_len = request.maxPathLen;
    }

    if (request.keyAlgorithm !== undefined) {
      body.key_algorithm = request.keyAlgorithm;
    }

    if (request.keySize !== undefined) {
      body.key_size = request.keySize;
    }

    if (request.metadata !== undefined && Object.keys(request.metadata).length > 0) {
      body.metadata = request.metadata;
    }

    const response = await this.#transport.post(`${CERTIFICATE_API_PATH}/issue`, body);

    return this.#transformIssueCertificateResponse(response);
  }

  /**
   * Sign a Certificate Signing Request (CSR).
   *
   * Signs a CSR with the QuicBaaS CA, producing a signed certificate.
   * Use this when you want to generate your own key pair externally.
   *
   * @param {string} csrPem - PEM-encoded Certificate Signing Request
   * @param {SignCSROptions} [options={}] - Signing options
   * @returns {Promise<SignCSRResponse>} The signed certificate
   * @throws {ValidationError} If csrPem is empty or options are invalid
   * @throws {AuthorizationError} If not authorized to sign CSRs
   *
   * @example
   * // Sign a CSR with default options
   * const cert = await certificate.signCSR(csrPem);
   *
   * @example
   * // Sign a CSR with custom validity and usage
   * const cert = await certificate.signCSR(csrPem, {
   *   validityDays: 180,
   *   usage: CertificateUsage.SERVER
   * });
   */
  async signCSR(csrPem, options = {}) {
    requiredString(csrPem, 'csrPem');
    this.#validateSignCSROptions(options);

    const body = {
      csr_pem: csrPem,
    };

    if (options.validityDays !== undefined) {
      body.validity_days = options.validityDays;
    }

    if (options.usage !== undefined) {
      body.usage = options.usage;
    }

    const response = await this.#transport.post(`${CERTIFICATE_API_PATH}/sign`, body);

    return this.#transformSignCSRResponse(response);
  }

  /**
   * Revoke a certificate.
   *
   * Marks a certificate as revoked by its serial number.
   * Revoked certificates will fail verification and will be
   * included in the Certificate Revocation List (CRL).
   *
   * @param {string} serialNumber - Certificate serial number to revoke
   * @returns {Promise<RevokeCertificateResponse>} The revocation result
   * @throws {ValidationError} If serialNumber is empty
   * @throws {NotFoundError} If the certificate is not found
   * @throws {AuthorizationError} If not authorized to revoke certificates
   *
   * @example
   * const result = await certificate.revokeCertificate('1234567890abcdef');
   * if (result.success) {
   *   console.log('Certificate revoked successfully');
   * }
   */
  async revokeCertificate(serialNumber) {
    requiredString(serialNumber, 'serialNumber');

    const body = {
      serial_number: serialNumber,
    };

    const response = await this.#transport.post(`${CERTIFICATE_API_PATH}/revoke`, body);

    return {
      success: response?.success ?? false,
    };
  }

  /**
   * Verify a certificate.
   *
   * Checks whether a certificate is valid, including checking
   * if it has been revoked, is within its validity period,
   * and has a valid signature chain.
   *
   * @param {string} certificatePem - PEM-encoded certificate to verify
   * @returns {Promise<VerifyCertificateResponse>} The verification result
   * @throws {ValidationError} If certificatePem is empty
   *
   * @example
   * const result = await certificate.verifyCertificate(certPem);
   * if (result.valid) {
   *   console.log('Certificate is valid');
   * } else {
   *   console.log('Certificate is invalid:', result.errorMessage);
   * }
   */
  async verifyCertificate(certificatePem) {
    requiredString(certificatePem, 'certificatePem');

    const body = {
      certificate_pem: certificatePem,
    };

    const response = await this.#transport.post(`${CERTIFICATE_API_PATH}/verify`, body);

    return this.#transformVerifyCertificateResponse(response);
  }

  /**
   * Get the CA certificate.
   *
   * Returns the Certificate Authority certificate and bundle.
   * Use this to configure clients and servers to trust certificates
   * issued by the QuicBaaS CA.
   *
   * @returns {Promise<GetCACertificateResponse>} The CA certificate and bundle
   *
   * @example
   * const ca = await certificate.getCACertificate();
   * console.log('CA Certificate:', ca.caCertificatePem);
   * console.log('CA Identity:', ca.identity);
   *
   * // Save for use in TLS configuration
   * fs.writeFileSync('ca.pem', ca.caCertificatePem);
   */
  async getCACertificate() {
    const response = await this.#transport.get(`${CERTIFICATE_API_PATH}/ca`);

    return this.#transformGetCACertificateResponse(response);
  }

  /**
   * Get TLS configuration.
   *
   * Returns TLS configuration parameters suitable for
   * configuring TLS connections. Note that in JavaScript
   * environments, this returns configuration options rather
   * than a native TLS config object.
   *
   * @param {GetTLSConfigOptions} [options={}] - TLS configuration options
   * @returns {Promise<GetTLSConfigResponse>} The TLS configuration
   * @throws {ValidationError} If options contain invalid values
   *
   * @example
   * // Get server TLS configuration
   * const tlsConfig = await certificate.getTLSConfig({
   *   serverName: 'api.example.com',
   *   isServer: true,
   *   requireClientCert: true,
   *   minVersion: TLSVersion.TLS_1_2
   * });
   */
  async getTLSConfig(options = {}) {
    this.#validateGetTLSConfigOptions(options);

    const body = {};

    if (options.serverName !== undefined) {
      body.server_name = options.serverName;
    }

    if (options.requireClientCert !== undefined) {
      body.require_client_cert = options.requireClientCert;
    }

    if (options.isServer !== undefined) {
      body.is_server = options.isServer;
    }

    if (options.minVersion !== undefined) {
      body.min_version = options.minVersion;
    }

    const response = await this.#transport.post(`${CERTIFICATE_API_PATH}/tls-config`, body);

    return this.#transformGetTLSConfigResponse(response);
  }

  /**
   * Validate issueCertificate request parameters.
   *
   * @param {IssueCertificateRequest} request - Request to validate
   * @throws {ValidationError} If parameters are invalid
   * @private
   */
  #validateIssueCertificateRequest(request) {
    required(request, 'request');
    requiredString(request.commonName, 'request.commonName');

    if (request.usage !== undefined) {
      oneOf(
        request.usage,
        [CertificateUsage.CLIENT, CertificateUsage.SERVER, CertificateUsage.BOTH],
        'request.usage'
      );
    }

    if (request.keyAlgorithm !== undefined) {
      oneOf(
        request.keyAlgorithm,
        [KeyAlgorithm.ECDSA, KeyAlgorithm.RSA],
        'request.keyAlgorithm'
      );
    }

    if (request.validityDays !== undefined) {
      nonNegativeInteger(request.validityDays, 'request.validityDays');
      if (request.validityDays === 0) {
        throw new ValidationError('validityDays must be greater than 0', 'request.validityDays');
      }
    }

    if (request.maxPathLen !== undefined) {
      nonNegativeInteger(request.maxPathLen, 'request.maxPathLen');
    }

    if (request.keySize !== undefined) {
      nonNegativeInteger(request.keySize, 'request.keySize');
      if (request.keySize === 0) {
        throw new ValidationError('keySize must be greater than 0', 'request.keySize');
      }
    }

    if (request.dnsNames !== undefined) {
      this.#validateStringArray(request.dnsNames, 'request.dnsNames');
    }

    if (request.ipAddresses !== undefined) {
      this.#validateStringArray(request.ipAddresses, 'request.ipAddresses');
    }

    if (request.emailAddresses !== undefined) {
      this.#validateStringArray(request.emailAddresses, 'request.emailAddresses');
    }

    if (request.uris !== undefined) {
      this.#validateStringArray(request.uris, 'request.uris');
    }

    if (request.metadata !== undefined) {
      if (typeof request.metadata !== 'object' || request.metadata === null || Array.isArray(request.metadata)) {
        throw new ValidationError('metadata must be an object', 'request.metadata');
      }
      for (const [key, value] of Object.entries(request.metadata)) {
        if (typeof key !== 'string' || key.trim() === '') {
          throw new ValidationError('metadata keys must be non-empty strings', 'request.metadata');
        }
        if (typeof value !== 'string') {
          throw new ValidationError(`metadata value for key '${key}' must be a string`, 'request.metadata');
        }
      }
    }
  }

  /**
   * Validate signCSR options.
   *
   * @param {SignCSROptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSignCSROptions(options) {
    if (options.usage !== undefined) {
      oneOf(
        options.usage,
        [CertificateUsage.CLIENT, CertificateUsage.SERVER, CertificateUsage.BOTH],
        'options.usage'
      );
    }

    if (options.validityDays !== undefined) {
      nonNegativeInteger(options.validityDays, 'options.validityDays');
      if (options.validityDays === 0) {
        throw new ValidationError('validityDays must be greater than 0', 'options.validityDays');
      }
    }
  }

  /**
   * Validate getTLSConfig options.
   *
   * @param {GetTLSConfigOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateGetTLSConfigOptions(options) {
    if (options.serverName !== undefined && typeof options.serverName !== 'string') {
      throw new ValidationError('serverName must be a string', 'options.serverName');
    }

    if (options.requireClientCert !== undefined && typeof options.requireClientCert !== 'boolean') {
      throw new ValidationError('requireClientCert must be a boolean', 'options.requireClientCert');
    }

    if (options.isServer !== undefined && typeof options.isServer !== 'boolean') {
      throw new ValidationError('isServer must be a boolean', 'options.isServer');
    }

    if (options.minVersion !== undefined) {
      oneOf(options.minVersion, [TLSVersion.TLS_1_2, TLSVersion.TLS_1_3], 'options.minVersion');
    }
  }

  /**
   * Validate an array of strings.
   *
   * @param {*} value - Value to validate
   * @param {string} fieldName - Field name for error messages
   * @throws {ValidationError} If value is not an array of strings
   * @private
   */
  #validateStringArray(value, fieldName) {
    if (!Array.isArray(value)) {
      throw new ValidationError(`${fieldName} must be an array`, fieldName);
    }

    for (let i = 0; i < value.length; i++) {
      if (typeof value[i] !== 'string') {
        throw new ValidationError(`${fieldName}[${i}] must be a string`, `${fieldName}[${i}]`);
      }
    }
  }

  /**
   * Transform server response to IssueCertificateResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {IssueCertificateResponse}
   * @private
   */
  #transformIssueCertificateResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid issue certificate response from server', 'response');
    }

    return {
      certificatePem: response.certificate_pem ?? response.certificatePem,
      privateKeyPem: response.private_key_pem ?? response.privateKeyPem,
      caCertificatePem: response.ca_certificate_pem ?? response.caCertificatePem,
      chainPem: response.chain_pem ?? response.chainPem,
      serialNumber: response.serial_number ?? response.serialNumber,
      notBefore: response.not_before ?? response.notBefore,
      notAfter: response.not_after ?? response.notAfter,
    };
  }

  /**
   * Transform server response to SignCSRResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {SignCSRResponse}
   * @private
   */
  #transformSignCSRResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid sign CSR response from server', 'response');
    }

    return {
      certificatePem: response.certificate_pem ?? response.certificatePem,
      caCertificatePem: response.ca_certificate_pem ?? response.caCertificatePem,
      chainPem: response.chain_pem ?? response.chainPem,
      serialNumber: response.serial_number ?? response.serialNumber,
      notBefore: response.not_before ?? response.notBefore,
      notAfter: response.not_after ?? response.notAfter,
    };
  }

  /**
   * Transform server response to VerifyCertificateResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {VerifyCertificateResponse}
   * @private
   */
  #transformVerifyCertificateResponse(response) {
    const result = {
      valid: response?.valid ?? false,
    };

    if (response?.error_message ?? response?.errorMessage) {
      result.errorMessage = response.error_message ?? response.errorMessage;
    }

    return result;
  }

  /**
   * Transform server response to GetCACertificateResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {GetCACertificateResponse}
   * @private
   */
  #transformGetCACertificateResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid CA certificate response from server', 'response');
    }

    return {
      caCertificatePem: response.ca_certificate_pem ?? response.caCertificatePem,
      caBundlePem: response.ca_bundle_pem ?? response.caBundlePem,
      identity: response.identity,
    };
  }

  /**
   * Transform server response to GetTLSConfigResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {GetTLSConfigResponse}
   * @private
   */
  #transformGetTLSConfigResponse(response) {
    return {
      serverName: response?.server_name ?? response?.serverName ?? null,
      requireClientCert: response?.require_client_cert ?? response?.requireClientCert ?? false,
      isServer: response?.is_server ?? response?.isServer ?? false,
      minVersion: response?.min_version ?? response?.minVersion ?? TLSVersion.TLS_1_2,
    };
  }
}

export default CertificateClient;

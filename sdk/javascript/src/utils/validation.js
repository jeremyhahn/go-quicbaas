/**
 * QuicBaaS JavaScript SDK - Validation Utilities
 *
 * @module @quicbaas/client/utils/validation
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';

/**
 * Validate that a value is not null or undefined.
 * @param {*} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function required(value, name) {
  if (value === null || value === undefined) {
    throw new ValidationError(`${name} is required`, name);
  }
}

/**
 * Validate that a string is not empty.
 * @param {string} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function requiredString(value, name) {
  required(value, name);
  if (typeof value !== 'string') {
    throw new ValidationError(`${name} must be a string`, name);
  }
  if (value.trim() === '') {
    throw new ValidationError(`${name} cannot be empty`, name);
  }
}

/**
 * Validate that a value is a non-negative integer.
 * @param {number} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function nonNegativeInteger(value, name) {
  if (value !== undefined && value !== null) {
    if (typeof value !== 'number' || !Number.isInteger(value) || value < 0) {
      throw new ValidationError(`${name} must be a non-negative integer`, name);
    }
  }
}

/**
 * Validate that a value is a positive integer.
 * @param {number} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function positiveInteger(value, name) {
  if (value !== undefined && value !== null) {
    if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0) {
      throw new ValidationError(`${name} must be a positive integer`, name);
    }
  }
}

/**
 * Validate that a value is an array.
 * @param {Array} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function requiredArray(value, name) {
  required(value, name);
  if (!Array.isArray(value)) {
    throw new ValidationError(`${name} must be an array`, name);
  }
}

/**
 * Validate that an array is not empty.
 * @param {Array} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function nonEmptyArray(value, name) {
  requiredArray(value, name);
  if (value.length === 0) {
    throw new ValidationError(`${name} cannot be empty`, name);
  }
}

/**
 * Validate that a value is one of allowed values.
 * @param {*} value - Value to check
 * @param {Array} allowed - Allowed values
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function oneOf(value, allowed, name) {
  if (value !== undefined && value !== null && !allowed.includes(value)) {
    throw new ValidationError(
      `${name} must be one of: ${allowed.join(', ')}`,
      name,
    );
  }
}

/**
 * Validate that a value is a valid object.
 * @param {Object} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function requiredObject(value, name) {
  required(value, name);
  if (typeof value !== 'object' || Array.isArray(value)) {
    throw new ValidationError(`${name} must be an object`, name);
  }
}

/**
 * Validate a byte array (Uint8Array or array of numbers).
 * @param {Uint8Array|Array<number>} value - Value to check
 * @param {string} name - Parameter name for error message
 * @throws {ValidationError}
 */
export function byteArray(value, name) {
  required(value, name);
  if (!(value instanceof Uint8Array) && !Array.isArray(value)) {
    throw new ValidationError(`${name} must be a byte array`, name);
  }
}

/**
 * Validate request object exists.
 * @param {Object} request - Request object
 * @throws {ValidationError}
 */
export function validateRequest(request) {
  if (!request || typeof request !== 'object') {
    throw new ValidationError('Request object is required', 'request');
  }
}

/**
 * Apply validation rules to request fields.
 * @param {Object} request - Request object
 * @param {Object} rules - Validation rules { fieldName: validatorFunction }
 * @throws {ValidationError}
 */
export function validateFields(request, rules) {
  validateRequest(request);
  for (const [field, validator] of Object.entries(rules)) {
    validator(request[field], field);
  }
}

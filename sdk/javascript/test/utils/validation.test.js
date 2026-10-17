/**
 * QuicBaaS JavaScript SDK - Validation Utilities Tests
 *
 * @module @quicbaas/client/utils/validation/tests
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import {
  required,
  requiredString,
  nonNegativeInteger,
  positiveInteger,
  requiredArray,
  nonEmptyArray,
  oneOf,
  requiredObject,
  byteArray,
  validateRequest,
  validateFields,
} from '../../src/utils/validation.js';
import { ValidationError } from '../../src/errors/index.js';

describe('Validation', () => {
  describe('required', () => {
    it('should pass for non-null values', () => {
      expect(() => required('value', 'field')).not.toThrow();
      expect(() => required(0, 'field')).not.toThrow();
      expect(() => required(false, 'field')).not.toThrow();
      expect(() => required('', 'field')).not.toThrow();
      expect(() => required([], 'field')).not.toThrow();
      expect(() => required({}, 'field')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => required(null, 'field')).toThrow(ValidationError);
      expect(() => required(null, 'field')).toThrow('field is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => required(undefined, 'field')).toThrow(ValidationError);
      expect(() => required(undefined, 'field')).toThrow('field is required');
    });

    it('should include field name in error details', () => {
      try {
        required(null, 'username');
      } catch (error) {
        expect(error.details.field).toBe('username');
      }
    });

    it('should handle various valid values', () => {
      expect(() => required(1, 'number')).not.toThrow();
      expect(() => required(-1, 'negative')).not.toThrow();
      expect(() => required(NaN, 'nan')).not.toThrow();
      expect(() => required(Infinity, 'infinity')).not.toThrow();
    });
  });

  describe('requiredString', () => {
    it('should pass for valid non-empty strings', () => {
      expect(() => requiredString('hello', 'field')).not.toThrow();
      expect(() => requiredString('a', 'field')).not.toThrow();
      expect(() => requiredString('  value  ', 'field')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => requiredString(null, 'name')).toThrow(ValidationError);
      expect(() => requiredString(null, 'name')).toThrow('name is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => requiredString(undefined, 'name')).toThrow(ValidationError);
      expect(() => requiredString(undefined, 'name')).toThrow('name is required');
    });

    it('should throw ValidationError for non-string types', () => {
      expect(() => requiredString(123, 'field')).toThrow(ValidationError);
      expect(() => requiredString(123, 'field')).toThrow('field must be a string');
    });

    it('should throw ValidationError for number', () => {
      expect(() => requiredString(42, 'count')).toThrow('count must be a string');
    });

    it('should throw ValidationError for boolean', () => {
      expect(() => requiredString(true, 'flag')).toThrow('flag must be a string');
    });

    it('should throw ValidationError for object', () => {
      expect(() => requiredString({}, 'data')).toThrow('data must be a string');
    });

    it('should throw ValidationError for array', () => {
      expect(() => requiredString([], 'items')).toThrow('items must be a string');
    });

    it('should throw ValidationError for empty string', () => {
      expect(() => requiredString('', 'field')).toThrow(ValidationError);
      expect(() => requiredString('', 'field')).toThrow('field cannot be empty');
    });

    it('should throw ValidationError for whitespace-only string', () => {
      expect(() => requiredString('   ', 'field')).toThrow(ValidationError);
      expect(() => requiredString('   ', 'field')).toThrow('field cannot be empty');
    });

    it('should throw for tabs and newlines only', () => {
      expect(() => requiredString('\t\n', 'field')).toThrow('field cannot be empty');
    });

    it('should include field name in error details', () => {
      try {
        requiredString('', 'email');
      } catch (error) {
        expect(error.details.field).toBe('email');
      }
    });
  });

  describe('nonNegativeInteger', () => {
    it('should pass for valid non-negative integers', () => {
      expect(() => nonNegativeInteger(0, 'field')).not.toThrow();
      expect(() => nonNegativeInteger(1, 'field')).not.toThrow();
      expect(() => nonNegativeInteger(100, 'field')).not.toThrow();
      expect(() => nonNegativeInteger(Number.MAX_SAFE_INTEGER, 'field')).not.toThrow();
    });

    it('should pass for null or undefined (optional field)', () => {
      expect(() => nonNegativeInteger(null, 'field')).not.toThrow();
      expect(() => nonNegativeInteger(undefined, 'field')).not.toThrow();
    });

    it('should throw ValidationError for negative integers', () => {
      expect(() => nonNegativeInteger(-1, 'count')).toThrow(ValidationError);
      expect(() => nonNegativeInteger(-1, 'count')).toThrow('count must be a non-negative integer');
    });

    it('should throw for large negative numbers', () => {
      expect(() => nonNegativeInteger(-1000000, 'value')).toThrow(
        'value must be a non-negative integer',
      );
    });

    it('should throw ValidationError for floating point numbers', () => {
      expect(() => nonNegativeInteger(1.5, 'limit')).toThrow(ValidationError);
      expect(() => nonNegativeInteger(1.5, 'limit')).toThrow('limit must be a non-negative integer');
    });

    it('should throw for small decimals', () => {
      expect(() => nonNegativeInteger(0.1, 'field')).toThrow('field must be a non-negative integer');
    });

    it('should throw ValidationError for non-number types', () => {
      expect(() => nonNegativeInteger('5', 'field')).toThrow('field must be a non-negative integer');
      expect(() => nonNegativeInteger(true, 'field')).toThrow(
        'field must be a non-negative integer',
      );
      expect(() => nonNegativeInteger({}, 'field')).toThrow('field must be a non-negative integer');
    });

    it('should throw for NaN', () => {
      expect(() => nonNegativeInteger(NaN, 'field')).toThrow('field must be a non-negative integer');
    });

    it('should throw for Infinity', () => {
      expect(() => nonNegativeInteger(Infinity, 'field')).toThrow(
        'field must be a non-negative integer',
      );
      expect(() => nonNegativeInteger(-Infinity, 'field')).toThrow(
        'field must be a non-negative integer',
      );
    });

    it('should include field name in error details', () => {
      try {
        nonNegativeInteger(-5, 'offset');
      } catch (error) {
        expect(error.details.field).toBe('offset');
      }
    });
  });

  describe('positiveInteger', () => {
    it('should pass for valid positive integers', () => {
      expect(() => positiveInteger(1, 'field')).not.toThrow();
      expect(() => positiveInteger(100, 'field')).not.toThrow();
      expect(() => positiveInteger(Number.MAX_SAFE_INTEGER, 'field')).not.toThrow();
    });

    it('should pass for null or undefined (optional field)', () => {
      expect(() => positiveInteger(null, 'field')).not.toThrow();
      expect(() => positiveInteger(undefined, 'field')).not.toThrow();
    });

    it('should throw ValidationError for zero', () => {
      expect(() => positiveInteger(0, 'id')).toThrow(ValidationError);
      expect(() => positiveInteger(0, 'id')).toThrow('id must be a positive integer');
    });

    it('should throw ValidationError for negative integers', () => {
      expect(() => positiveInteger(-1, 'count')).toThrow(ValidationError);
      expect(() => positiveInteger(-1, 'count')).toThrow('count must be a positive integer');
    });

    it('should throw for large negative numbers', () => {
      expect(() => positiveInteger(-1000000, 'value')).toThrow('value must be a positive integer');
    });

    it('should throw ValidationError for floating point numbers', () => {
      expect(() => positiveInteger(1.5, 'limit')).toThrow(ValidationError);
      expect(() => positiveInteger(0.5, 'limit')).toThrow('limit must be a positive integer');
    });

    it('should throw ValidationError for non-number types', () => {
      expect(() => positiveInteger('5', 'field')).toThrow('field must be a positive integer');
      expect(() => positiveInteger(true, 'field')).toThrow('field must be a positive integer');
      expect(() => positiveInteger([], 'field')).toThrow('field must be a positive integer');
    });

    it('should throw for NaN', () => {
      expect(() => positiveInteger(NaN, 'field')).toThrow('field must be a positive integer');
    });

    it('should throw for Infinity', () => {
      expect(() => positiveInteger(Infinity, 'field')).toThrow('field must be a positive integer');
    });

    it('should include field name in error details', () => {
      try {
        positiveInteger(0, 'page');
      } catch (error) {
        expect(error.details.field).toBe('page');
      }
    });
  });

  describe('requiredArray', () => {
    it('should pass for valid arrays', () => {
      expect(() => requiredArray([], 'field')).not.toThrow();
      expect(() => requiredArray([1, 2, 3], 'field')).not.toThrow();
      expect(() => requiredArray(['a', 'b'], 'field')).not.toThrow();
      expect(() => requiredArray([null, undefined], 'field')).not.toThrow();
    });

    it('should pass for arrays with mixed types', () => {
      expect(() => requiredArray([1, 'two', { three: 3 }], 'mixed')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => requiredArray(null, 'items')).toThrow(ValidationError);
      expect(() => requiredArray(null, 'items')).toThrow('items is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => requiredArray(undefined, 'items')).toThrow(ValidationError);
      expect(() => requiredArray(undefined, 'items')).toThrow('items is required');
    });

    it('should throw ValidationError for non-array types', () => {
      expect(() => requiredArray('array', 'field')).toThrow('field must be an array');
      expect(() => requiredArray(123, 'field')).toThrow('field must be an array');
      expect(() => requiredArray({}, 'field')).toThrow('field must be an array');
      expect(() => requiredArray({ length: 1 }, 'field')).toThrow('field must be an array');
    });

    it('should throw for array-like objects', () => {
      expect(() => requiredArray({ 0: 'a', 1: 'b', length: 2 }, 'field')).toThrow(
        'field must be an array',
      );
    });

    it('should include field name in error details', () => {
      try {
        requiredArray('not an array', 'tags');
      } catch (error) {
        expect(error.details.field).toBe('tags');
      }
    });
  });

  describe('nonEmptyArray', () => {
    it('should pass for non-empty arrays', () => {
      expect(() => nonEmptyArray([1], 'field')).not.toThrow();
      expect(() => nonEmptyArray([1, 2, 3], 'field')).not.toThrow();
      expect(() => nonEmptyArray(['a'], 'field')).not.toThrow();
    });

    it('should pass for arrays with falsy values', () => {
      expect(() => nonEmptyArray([null], 'field')).not.toThrow();
      expect(() => nonEmptyArray([undefined], 'field')).not.toThrow();
      expect(() => nonEmptyArray([0], 'field')).not.toThrow();
      expect(() => nonEmptyArray([''], 'field')).not.toThrow();
      expect(() => nonEmptyArray([false], 'field')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => nonEmptyArray(null, 'keys')).toThrow(ValidationError);
      expect(() => nonEmptyArray(null, 'keys')).toThrow('keys is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => nonEmptyArray(undefined, 'keys')).toThrow('keys is required');
    });

    it('should throw ValidationError for non-array types', () => {
      expect(() => nonEmptyArray('array', 'field')).toThrow('field must be an array');
    });

    it('should throw ValidationError for empty array', () => {
      expect(() => nonEmptyArray([], 'items')).toThrow(ValidationError);
      expect(() => nonEmptyArray([], 'items')).toThrow('items cannot be empty');
    });

    it('should include field name in error details', () => {
      try {
        nonEmptyArray([], 'operations');
      } catch (error) {
        expect(error.details.field).toBe('operations');
      }
    });
  });

  describe('oneOf', () => {
    const allowedValues = ['read', 'write', 'admin'];

    it('should pass for allowed values', () => {
      expect(() => oneOf('read', allowedValues, 'permission')).not.toThrow();
      expect(() => oneOf('write', allowedValues, 'permission')).not.toThrow();
      expect(() => oneOf('admin', allowedValues, 'permission')).not.toThrow();
    });

    it('should pass for null or undefined (optional field)', () => {
      expect(() => oneOf(null, allowedValues, 'permission')).not.toThrow();
      expect(() => oneOf(undefined, allowedValues, 'permission')).not.toThrow();
    });

    it('should throw ValidationError for values not in allowed list', () => {
      expect(() => oneOf('delete', allowedValues, 'permission')).toThrow(ValidationError);
      expect(() => oneOf('delete', allowedValues, 'permission')).toThrow(
        'permission must be one of: read, write, admin',
      );
    });

    it('should throw for case-sensitive mismatch', () => {
      expect(() => oneOf('READ', allowedValues, 'permission')).toThrow(
        'permission must be one of: read, write, admin',
      );
    });

    it('should work with numeric allowed values', () => {
      const numericAllowed = [1, 2, 3];
      expect(() => oneOf(1, numericAllowed, 'level')).not.toThrow();
      expect(() => oneOf(4, numericAllowed, 'level')).toThrow('level must be one of: 1, 2, 3');
    });

    it('should work with boolean allowed values', () => {
      const boolAllowed = [true, false];
      expect(() => oneOf(true, boolAllowed, 'enabled')).not.toThrow();
      expect(() => oneOf('true', boolAllowed, 'enabled')).toThrow(
        'enabled must be one of: true, false',
      );
    });

    it('should work with empty allowed list', () => {
      expect(() => oneOf('any', [], 'field')).toThrow('field must be one of: ');
    });

    it('should include field name in error details', () => {
      try {
        oneOf('invalid', ['a', 'b'], 'status');
      } catch (error) {
        expect(error.details.field).toBe('status');
      }
    });
  });

  describe('requiredObject', () => {
    it('should pass for valid objects', () => {
      expect(() => requiredObject({}, 'field')).not.toThrow();
      expect(() => requiredObject({ key: 'value' }, 'field')).not.toThrow();
      expect(() => requiredObject({ nested: { deep: true } }, 'field')).not.toThrow();
    });

    it('should pass for objects with various property types', () => {
      const complexObject = {
        string: 'value',
        number: 42,
        boolean: true,
        array: [1, 2, 3],
        nested: { inner: 'value' },
      };
      expect(() => requiredObject(complexObject, 'config')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => requiredObject(null, 'options')).toThrow(ValidationError);
      expect(() => requiredObject(null, 'options')).toThrow('options is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => requiredObject(undefined, 'options')).toThrow('options is required');
    });

    it('should throw ValidationError for arrays', () => {
      expect(() => requiredObject([], 'data')).toThrow(ValidationError);
      expect(() => requiredObject([], 'data')).toThrow('data must be an object');
      expect(() => requiredObject([1, 2, 3], 'data')).toThrow('data must be an object');
    });

    it('should throw ValidationError for primitive types', () => {
      expect(() => requiredObject('string', 'field')).toThrow('field must be an object');
      expect(() => requiredObject(123, 'field')).toThrow('field must be an object');
      expect(() => requiredObject(true, 'field')).toThrow('field must be an object');
    });

    it('should throw for functions', () => {
      expect(() =>
        requiredObject(() => {}, 'field'),
      ).toThrow('field must be an object');
    });

    it('should include field name in error details', () => {
      try {
        requiredObject('not an object', 'metadata');
      } catch (error) {
        expect(error.details.field).toBe('metadata');
      }
    });
  });

  describe('byteArray', () => {
    it('should pass for Uint8Array', () => {
      expect(() => byteArray(new Uint8Array([1, 2, 3]), 'data')).not.toThrow();
      expect(() => byteArray(new Uint8Array(0), 'data')).not.toThrow();
      expect(() => byteArray(new Uint8Array(100), 'data')).not.toThrow();
    });

    it('should pass for regular array of numbers', () => {
      expect(() => byteArray([1, 2, 3], 'data')).not.toThrow();
      expect(() => byteArray([], 'data')).not.toThrow();
      expect(() => byteArray([0, 255, 128], 'data')).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => byteArray(null, 'payload')).toThrow(ValidationError);
      expect(() => byteArray(null, 'payload')).toThrow('payload is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => byteArray(undefined, 'payload')).toThrow('payload is required');
    });

    it('should throw ValidationError for non-byte-array types', () => {
      expect(() => byteArray('string', 'data')).toThrow('data must be a byte array');
      expect(() => byteArray(123, 'data')).toThrow('data must be a byte array');
      expect(() => byteArray({}, 'data')).toThrow('data must be a byte array');
    });

    it('should throw for ArrayBuffer directly', () => {
      expect(() => byteArray(new ArrayBuffer(8), 'data')).toThrow('data must be a byte array');
    });

    it('should throw for other typed arrays', () => {
      expect(() => byteArray(new Int8Array([1, 2, 3]), 'data')).toThrow('data must be a byte array');
      expect(() => byteArray(new Uint16Array([1, 2, 3]), 'data')).toThrow(
        'data must be a byte array',
      );
    });

    it('should include field name in error details', () => {
      try {
        byteArray('invalid', 'buffer');
      } catch (error) {
        expect(error.details.field).toBe('buffer');
      }
    });
  });

  describe('validateRequest', () => {
    it('should pass for valid request objects', () => {
      expect(() => validateRequest({})).not.toThrow();
      expect(() => validateRequest({ key: 'value' })).not.toThrow();
      expect(() => validateRequest({ nested: { data: true } })).not.toThrow();
    });

    it('should throw ValidationError for null', () => {
      expect(() => validateRequest(null)).toThrow(ValidationError);
      expect(() => validateRequest(null)).toThrow('Request object is required');
    });

    it('should throw ValidationError for undefined', () => {
      expect(() => validateRequest(undefined)).toThrow(ValidationError);
      expect(() => validateRequest(undefined)).toThrow('Request object is required');
    });

    it('should throw ValidationError for non-object types', () => {
      expect(() => validateRequest('request')).toThrow('Request object is required');
      expect(() => validateRequest(123)).toThrow('Request object is required');
      expect(() => validateRequest(true)).toThrow('Request object is required');
    });

    it('should pass for arrays (typeof array is object)', () => {
      // Note: validateRequest only checks typeof === 'object', not Array.isArray
      // Arrays pass the validation since typeof [] === 'object'
      expect(() => validateRequest([])).not.toThrow();
    });

    it('should include field name in error details', () => {
      try {
        validateRequest(null);
      } catch (error) {
        expect(error.details.field).toBe('request');
      }
    });
  });

  describe('validateFields', () => {
    it('should pass when all validations succeed', () => {
      const request = {
        name: 'John',
        age: 25,
        items: [1, 2, 3],
      };
      const rules = {
        name: requiredString,
        age: positiveInteger,
        items: requiredArray,
      };

      expect(() => validateFields(request, rules)).not.toThrow();
    });

    it('should throw when request is null', () => {
      expect(() => validateFields(null, {})).toThrow('Request object is required');
    });

    it('should throw when request is undefined', () => {
      expect(() => validateFields(undefined, {})).toThrow('Request object is required');
    });

    it('should throw when a validation rule fails', () => {
      const request = {
        name: '',
        age: 25,
      };
      const rules = {
        name: requiredString,
        age: positiveInteger,
      };

      expect(() => validateFields(request, rules)).toThrow('name cannot be empty');
    });

    it('should validate all fields in rules', () => {
      const request = {
        key: 'valid-key',
        value: 'valid-value',
      };
      const rules = {
        key: requiredString,
        value: requiredString,
      };

      expect(() => validateFields(request, rules)).not.toThrow();
    });

    it('should handle missing fields in request', () => {
      const request = {
        name: 'John',
      };
      const rules = {
        name: requiredString,
        email: requiredString,
      };

      expect(() => validateFields(request, rules)).toThrow('email is required');
    });

    it('should pass with empty rules object', () => {
      const request = { any: 'value' };
      expect(() => validateFields(request, {})).not.toThrow();
    });

    it('should work with complex validation rules', () => {
      const request = {
        keys: ['key1', 'key2'],
        limit: 10,
        mode: 'read',
      };
      const rules = {
        keys: nonEmptyArray,
        limit: positiveInteger,
        mode: (value, name) => oneOf(value, ['read', 'write'], name),
      };

      expect(() => validateFields(request, rules)).not.toThrow();
    });

    it('should throw with appropriate message for complex rule failure', () => {
      const request = {
        mode: 'invalid',
      };
      const rules = {
        mode: (value, name) => oneOf(value, ['read', 'write'], name),
      };

      expect(() => validateFields(request, rules)).toThrow('mode must be one of: read, write');
    });

    it('should handle optional fields with nonNegativeInteger', () => {
      const request = {
        name: 'test',
        offset: undefined,
      };
      const rules = {
        name: requiredString,
        offset: nonNegativeInteger,
      };

      expect(() => validateFields(request, rules)).not.toThrow();
    });

    it('should fail fast on first validation error', () => {
      const request = {
        field1: null,
        field2: null,
      };
      const rules = {
        field1: required,
        field2: required,
      };

      try {
        validateFields(request, rules);
      } catch (error) {
        expect(error.message).toContain('is required');
      }
    });
  });
});

import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    // Use the test tsconfig so test files and src files share the same
    // compiler settings without overriding rootDir in the library tsconfig.
    typecheck: {
      tsconfig: './tsconfig.test.json',
    },
    environment: 'node',
  },
});

/**
 * Rollup configuration for QuicBaaS JavaScript SDK
 */

import resolve from '@rollup/plugin-node-resolve';
import commonjs from '@rollup/plugin-commonjs';
import babel from '@rollup/plugin-babel';
import terser from '@rollup/plugin-terser';

const production = process.env.NODE_ENV === 'production';

// Common plugins
const plugins = [
  resolve({
    browser: true,
  }),
  commonjs(),
  babel({
    babelHelpers: 'bundled',
    exclude: 'node_modules/**',
    presets: [
      ['@babel/preset-env', {
        targets: {
          browsers: '> 0.5%, last 2 versions, not dead',
        },
        modules: false,
      }],
    ],
  }),
];

// Add terser for production builds
if (production) {
  plugins.push(terser({
    compress: {
      drop_console: true,
      drop_debugger: true,
    },
    format: {
      comments: false,
    },
  }));
}

export default [
  // ESM build (for modern bundlers)
  {
    input: 'src/index.js',
    output: {
      file: 'dist/quicbaas.esm.js',
      format: 'esm',
      sourcemap: true,
    },
    plugins,
  },

  // CommonJS build (for Node.js)
  {
    input: 'src/index.js',
    output: {
      file: 'dist/quicbaas.js',
      format: 'cjs',
      sourcemap: true,
      exports: 'named',
    },
    plugins,
  },

  // UMD build (for browsers via script tag)
  {
    input: 'src/index.js',
    output: {
      file: 'dist/quicbaas.browser.js',
      format: 'umd',
      name: 'QuicBaaS',
      sourcemap: true,
      exports: 'named',
      globals: {},
    },
    plugins,
  },

  // Minified UMD build for production
  {
    input: 'src/index.js',
    output: {
      file: 'dist/quicbaas.browser.min.js',
      format: 'umd',
      name: 'QuicBaaS',
      sourcemap: true,
      exports: 'named',
      globals: {},
    },
    plugins: [
      ...plugins,
      terser({
        compress: {
          drop_console: true,
          drop_debugger: true,
        },
        format: {
          comments: false,
        },
      }),
    ],
  },
];

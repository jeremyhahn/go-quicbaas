'use strict';

// Runs jest under a Node with its built-in web storage off. Node 26 defines
// its own localStorage/sessionStorage globals, which shadow jsdom's
// window.localStorage. An older Node (as in the CI devcontainer) rejects the
// flag, so it is passed only to a Node that knows it.
const { spawnSync } = require('node:child_process');
const path = require('node:path');

// jest's package exports do not include bin/jest.js, so require.resolve
// refuses it; use the path the scripts used before.
const jestBin = path.join(__dirname, '..', 'node_modules', 'jest', 'bin', 'jest.js');

const flags = process.allowedNodeEnvironmentFlags.has('--experimental-webstorage')
  ? ['--no-experimental-webstorage']
  : [];

const result = spawnSync(
  process.execPath,
  [...flags, jestBin, ...process.argv.slice(2)],
  { stdio: 'inherit' },
);

process.exit(result.status === null ? 1 : result.status);

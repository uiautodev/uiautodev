'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { parseArgs } = require('../src/args.js');

test('parses nothing into defaults', () => {
  assert.deepEqual(parseArgs([]), {
    version: null,
    force: false,
    installOnly: false,
    help: false,
    passthrough: [],
  });
});

test('parses --help', () => {
  assert.equal(parseArgs(['--help']).help, true);
});

test('parses --force and -f', () => {
  assert.equal(parseArgs(['--force']).force, true);
  assert.equal(parseArgs(['-f']).force, true);
});

test('parses --install-only', () => {
  assert.equal(parseArgs(['--install-only']).installOnly, true);
});

test('parses --version <v>', () => {
  const opts = parseArgs(['--version', '0.6.0']);
  assert.equal(opts.version, '0.6.0');
});

test('parses --version=<v>', () => {
  const opts = parseArgs(['--version=0.6.0']);
  assert.equal(opts.version, '0.6.0');
});

test('throws when --version has no value', () => {
  assert.throws(() => parseArgs(['--version']));
});

test('everything after -- goes to passthrough untouched', () => {
  const opts = parseArgs(['--force', '--', '--version', 'x', '--port', '9000']);
  assert.equal(opts.force, true);
  assert.equal(opts.version, null);
  assert.deepEqual(opts.passthrough, ['--version', 'x', '--port', '9000']);
});

test('non-cli args before -- are passthrough too', () => {
  const opts = parseArgs(['--port', '9000', '--force']);
  assert.deepEqual(opts.passthrough, ['--port', '9000']);
  assert.equal(opts.force, true);
});

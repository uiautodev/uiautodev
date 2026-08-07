'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { parseArgs } = require('../src/args.js');

test('no args defaults to run', () => {
  const opts = parseArgs([]);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, []);
});

test('run command', () => {
  const opts = parseArgs(['run']);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, []);
});

test('run passes everything after it verbatim', () => {
  const opts = parseArgs(['run', '-addr', ':9000', '--verbose']);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, ['-addr', ':9000', '--verbose']);
});

test('global flags before run plus passthrough', () => {
  const opts = parseArgs(['--version', '0.6.0', '--force', 'run', '-addr', ':9000']);
  assert.equal(opts.version, '0.6.0');
  assert.equal(opts.force, true);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, ['-addr', ':9000']);
});

test('--version=... form', () => {
  const opts = parseArgs(['--version=0.6.0', 'run']);
  assert.equal(opts.version, '0.6.0');
});

test('download command', () => {
  const opts = parseArgs(['download']);
  assert.equal(opts.command, 'download');
});

test('download accepts global flags after it', () => {
  const opts = parseArgs(['download', '--force', '--version', '0.6.0']);
  assert.equal(opts.command, 'download');
  assert.equal(opts.force, true);
  assert.equal(opts.version, '0.6.0');
});

test('global flags before download', () => {
  const opts = parseArgs(['--force', 'download']);
  assert.equal(opts.command, 'download');
  assert.equal(opts.force, true);
});

test('--help flag sets help', () => {
  const opts = parseArgs(['--help']);
  assert.equal(opts.help, true);
});

test('-h flag sets help', () => {
  const opts = parseArgs(['-h']);
  assert.equal(opts.help, true);
});

test('help command sets help', () => {
  const opts = parseArgs(['help']);
  assert.equal(opts.help, true);
});

test('flags before subcommand are parsed, not passed through', () => {
  const opts = parseArgs(['--force', 'run', '--port', '1']);
  assert.equal(opts.force, true);
  assert.deepEqual(opts.passthrough, ['--port', '1']);
});

test('defined global flag after run is passed through, not consumed', () => {
  const opts = parseArgs(['run', '--force']);
  assert.equal(opts.force, false);
  assert.deepEqual(opts.passthrough, ['--force']);
});

test('bare args without subcommand default to run passthrough', () => {
  const opts = parseArgs(['-addr', ':9000']);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, ['-addr', ':9000']);
});

test('unknown bare word defaults to run passthrough', () => {
  const opts = parseArgs(['foo']);
  assert.equal(opts.command, 'run');
  assert.deepEqual(opts.passthrough, ['foo']);
});

test('throws when --version has no value', () => {
  assert.throws(() => parseArgs(['--version']));
});

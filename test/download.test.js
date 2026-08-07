'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const os = require('node:os');
const path = require('node:path');

const { getCacheDir, formatBytes } = require('../src/index.js');

test('getCacheDir defaults to ~/.cache/uiautodev/{version}', () => {
  assert.equal(
    getCacheDir('0.6.0'),
    path.join(os.homedir(), '.cache', 'uiautodev', '0.6.0')
  );
});

test('getCacheDir honors UIAUTODEV_CACHE_DIR', () => {
  process.env.UIAUTODEV_CACHE_DIR = '/tmp/uiauto-cache';
  try {
    assert.equal(getCacheDir('0.6.0'), path.join('/tmp/uiauto-cache', '0.6.0'));
  } finally {
    delete process.env.UIAUTODEV_CACHE_DIR;
  }
});

test('formatBytes', () => {
  assert.equal(formatBytes(0), '0 B');
  assert.equal(formatBytes(1024), '1.0 KB');
  assert.equal(formatBytes(18157520), '17.3 MB');
});

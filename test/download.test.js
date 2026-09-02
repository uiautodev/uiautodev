'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const os = require('node:os');
const path = require('node:path');

const { getCacheDir } = require('../src/cache.js');
const { formatBytes } = require('../src/download.js');
const { downloadStatUrl } = require('../src/api.js');

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

test('downloadStatUrl builds the stats endpoint', () => {
  assert.equal(
    downloadStatUrl('0.7.2', 'uiautodev-server-darwin-arm64-0.6.0'),
    'https://download.devsleep.com/api/versions/0.7.2/files/uiautodev-server-darwin-arm64-0.6.0/downloads'
  );
});

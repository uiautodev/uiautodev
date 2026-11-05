'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { createLogger, enableDebug, isDebugEnabled } = require('../src/log.js');

test('createLogger returns a function', () => {
  const log = createLogger('main');
  assert.equal(typeof log, 'function');
});

test('isDebugEnabled defaults to false', () => {
  const prev = process.env.DEBUG;
  delete process.env.DEBUG;
  try {
    assert.equal(isDebugEnabled(), false);
  } finally {
    if (prev) process.env.DEBUG = prev;
  }
});

test('enableDebug turns on uiautodev debug', () => {
  enableDebug();
  assert.equal(isDebugEnabled(), true);
});

test('DEBUG env mentioning uiautodev is detected', () => {
  const prev = process.env.DEBUG;
  process.env.DEBUG = 'uiautodev:post';
  try {
    assert.equal(isDebugEnabled(), true);
  } finally {
    if (prev) process.env.DEBUG = prev;
  }
});

'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { quotePath } = require('../src/shell.js');

test('quotePath leaves simple paths unquoted', () => {
  assert.equal(quotePath('my-plugin'), 'my-plugin');
  assert.equal(quotePath('sub/dir'), 'sub/dir');
  assert.equal(quotePath('a@b+c=1,2:3'), 'a@b+c=1,2:3');
});

test('quotePath quotes paths with spaces', () => {
  assert.equal(quotePath('my plugin', 'linux'), "'my plugin'");
  assert.equal(quotePath('my plugin', 'win32'), '"my plugin"');
});

test('quotePath escapes single quotes on posix', () => {
  assert.equal(quotePath("it's", 'linux'), "'it'\\''s'");
});

test('quotePath escapes double quotes on windows', () => {
  assert.equal(quotePath('a"b', 'win32'), '"a""b"');
});

test('quotePath handles windows backslash separators', () => {
  assert.equal(quotePath('sub\\dir', 'win32'), '"sub\\dir"');
});

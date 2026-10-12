'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const {
  listTemplates,
  getTemplate,
  buildTarballUrl,
} = require('../src/templates.js');

test('lists the preact template', () => {
  const templates = listTemplates();
  const preact = templates.find((t) => t.name === 'preact');
  assert.ok(preact);
  assert.equal(preact.repo, 'uiautodev-plugins/preact-template');
  assert.match(preact.tarball, /^https:\/\/codeload\.github\.com\//);
});

test('getTemplate returns the preact template', () => {
  const template = getTemplate('preact');
  assert.equal(template.branch, 'master');
  assert.equal(
    template.tarball,
    'https://codeload.github.com/uiautodev-plugins/preact-template/tar.gz/refs/heads/master'
  );
});

test('getTemplate throws for unknown templates', () => {
  assert.throws(() => getTemplate('nope'), /Unknown template/);
});

test('buildTarballUrl builds a codeload archive url', () => {
  assert.equal(
    buildTarballUrl({ repo: 'foo/bar', branch: 'main' }),
    'https://codeload.github.com/foo/bar/tar.gz/refs/heads/main'
  );
});

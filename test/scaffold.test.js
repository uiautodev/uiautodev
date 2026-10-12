'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { isEmptyDir, scaffold, replaceDir } = require('../src/scaffold.js');

function makeTempDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'uiautodev-scaffold-'));
}

test('isEmptyDir reports emptiness', () => {
  const dir = makeTempDir();
  try {
    assert.equal(isEmptyDir(dir), true);
    fs.writeFileSync(path.join(dir, 'file.txt'), 'x');
    assert.equal(isEmptyDir(dir), false);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('scaffold refuses a non-empty target without force', async () => {
  const dir = makeTempDir();
  fs.writeFileSync(path.join(dir, 'file.txt'), 'x');
  try {
    await assert.rejects(
      scaffold({
        template: { name: 'x', tarball: 'https://example.com/x.tar.gz' },
        name: '.',
        force: false,
        cwd: dir,
      }),
      /not empty/
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('replaceDir moves staging into place when target is absent', () => {
  const dir = makeTempDir();
  const target = path.join(dir, 'target');
  const staging = fs.mkdtempSync(path.join(dir, '.target.uiautodev-'));
  fs.writeFileSync(path.join(staging, 'app.tsx'), 'new');
  try {
    replaceDir(staging, target);
    assert.equal(fs.readFileSync(path.join(target, 'app.tsx'), 'utf8'), 'new');
    assert.equal(fs.existsSync(staging), false);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

test('replaceDir removes stale files from an existing target', () => {
  const dir = makeTempDir();
  const target = path.join(dir, 'target');
  fs.mkdirSync(target);
  fs.writeFileSync(path.join(target, 'stale.txt'), 'old');
  fs.writeFileSync(path.join(target, 'app.tsx'), 'old');
  const staging = fs.mkdtempSync(path.join(dir, '.target.uiautodev-'));
  fs.writeFileSync(path.join(staging, 'app.tsx'), 'new');
  try {
    replaceDir(staging, target);
    assert.equal(fs.existsSync(path.join(target, 'stale.txt')), false);
    assert.equal(fs.readFileSync(path.join(target, 'app.tsx'), 'utf8'), 'new');
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});


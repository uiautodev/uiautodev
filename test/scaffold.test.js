'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { isEmptyDir, scaffold } = require('../src/scaffold.js');

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

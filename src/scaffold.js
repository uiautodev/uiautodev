'use strict';

const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { downloadTo } = require('./download');
const { createLogger } = require('./log');

const log = createLogger('scaffold');

function isEmptyDir(dir) {
  return fs.readdirSync(dir).length === 0;
}

function extractTarball(tarball, targetDir) {
  const result = spawnSync(
    'tar',
    ['-xzf', tarball, '-C', targetDir, '--strip-components=1'],
    { stdio: 'inherit' }
  );
  if (result.error) {
    throw new Error(
      `Failed to extract template (is "tar" installed?): ${result.error.message}`
    );
  }
  if (result.status !== 0) {
    throw new Error(`Failed to extract template: tar exited with ${result.status}`);
  }
}

async function scaffold({ template, name, force, cwd = process.cwd() }) {
  const targetDir = path.resolve(cwd, name);

  if (fs.existsSync(targetDir)) {
    if (!fs.statSync(targetDir).isDirectory()) {
      throw new Error(`Target already exists and is not a directory: ${targetDir}`);
    }
    if (!isEmptyDir(targetDir) && !force) {
      throw new Error(
        `Target directory is not empty: ${targetDir} (use --force to overwrite)`
      );
    }
  }

  fs.mkdirSync(targetDir, { recursive: true });

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'uiautodev-create-'));
  const tarball = path.join(tmpDir, 'template.tar.gz');
  try {
    process.stderr.write(`Downloading template "${template.name}"...\n`);
    log('downloading %s -> %s', template.tarball, tarball);
    await downloadTo(template.tarball, tarball);
    extractTarball(tarball, targetDir);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }

  return targetDir;
}

module.exports = { scaffold, extractTarball, isEmptyDir };

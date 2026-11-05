'use strict';

const os = require('node:os');
const path = require('node:path');

function getCacheDir(version) {
  const base =
    process.env.UIAUTODEV_CACHE_DIR ||
    path.join(os.homedir(), '.cache', 'uiautodev');
  return path.join(base, version);
}

module.exports = { getCacheDir };

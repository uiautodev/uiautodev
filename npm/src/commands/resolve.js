'use strict';

const path = require('node:path');

const { getLatestVersion, getVersionFiles } = require('../api');
const { getCacheDir } = require('../cache');
const { resolveBinary, describePlatform } = require('../platform');
const { createLogger } = require('../log');

const mainLog = createLogger('main');

async function resolveTarget(opts) {
  const version = opts.version || (await getLatestVersion());
  const { files } = await getVersionFiles(version);

  const binary = resolveBinary({
    platform: process.platform,
    arch: process.arch,
    files,
  });
  if (!binary) {
    throw new Error(
      `No server binary available for ${describePlatform(
        process.platform,
        process.arch
      )} in version ${version}`
    );
  }

  const binPath = path.join(getCacheDir(version), binary.name);
  mainLog('resolved version=%s binary=%s', version, binary.name);

  return { version, files, binary, binPath };
}

module.exports = { resolveTarget };

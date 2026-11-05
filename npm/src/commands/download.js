'use strict';

const { ensureBinary } = require('../download');
const { resolveTarget } = require('./resolve');

async function downloadCommand(opts) {
  const { version, binary } = await resolveTarget(opts);
  const binPath = await ensureBinary({ binary, version, force: opts.force });
  console.log(binPath);
}

module.exports = downloadCommand;

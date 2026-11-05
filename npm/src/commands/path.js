'use strict';

const { resolveTarget } = require('./resolve');

async function pathCommand(opts) {
  const { binPath } = await resolveTarget(opts);
  console.log(binPath);
}

module.exports = pathCommand;

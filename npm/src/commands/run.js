'use strict';

const { spawn } = require('node:child_process');

const { ensureBinary } = require('../download');
const { resolveTarget } = require('./resolve');

async function runCommand(opts) {
  const { version, binary } = await resolveTarget(opts);
  const binPath = await ensureBinary({ binary, version, force: opts.force });

  await new Promise((resolve) => {
    const proc = spawn(binPath, opts.passthrough, { stdio: 'inherit' });
    for (const sig of ['SIGINT', 'SIGTERM']) {
      process.on(sig, () => proc.kill(sig));
    }
    proc.on('error', (err) => {
      process.stderr.write(`uiautodev: failed to start server: ${err.message}\n`);
      process.exitCode = 1;
      resolve();
    });
    proc.on('exit', (code, signal) => {
      if (signal) process.exitCode = 1;
      else if (code != null) process.exitCode = code;
      resolve();
    });
  });
}

module.exports = runCommand;

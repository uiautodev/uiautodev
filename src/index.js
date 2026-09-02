'use strict';

const { parseArgs } = require('./args');
const { enableDebug } = require('./log');
const commands = require('./commands');

async function run(argv) {
  const opts = parseArgs(argv);

  if (opts.debug) {
    enableDebug();
  }

  if (opts.help) {
    return;
  }

  const handler = commands[opts.command] || commands.run;
  return handler(opts);
}

module.exports = { run };

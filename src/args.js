'use strict';

const { Command } = require('commander');

function parseArgs(argv) {
  const result = {
    command: 'run',
    version: null,
    force: false,
    help: false,
    passthrough: [],
  };

  const program = new Command()
    .name('uiautodev')
    .usage('[options] <command> [args]')
    .description('Download the UIAutoDev server binary and run it.')
    .option('--version <v>', 'Use a specific version (default: latest)')
    .option('-f, --force', 'Force re-download even if cached')
    .argument('[args...]', 'arguments passed to the server binary')
    .allowUnknownOption(true)
    .enablePositionalOptions();

  program
    .command('run')
    .helpOption(false)
    .description('Download (if needed) and run the server binary (default)')
    .argument('[args...]', 'arguments passed to the server binary')
    .passThroughOptions()
    .allowUnknownOption(true)
    .action((args) => {
      result.passthrough = args;
    });

  program
    .command('download')
    .description('Download the server binary only and print its path')
    .option('--version <v>', 'Use a specific version (default: latest)')
    .option('-f, --force', 'Force re-download even if cached')
    .action((opts) => {
      result.command = 'download';
      result.force = result.force || Boolean(opts.force);
      result.version = result.version || opts.version || null;
    });

  program
    .command('help')
    .description('Show help')
    .action(() => {
      program.help();
    });

  program.action((args) => {
    result.passthrough = args;
  });

  program.addHelpText(
    'beforeAll',
    'Examples:\n  uiautodev run -addr :9000\n  uiautodev download\n'
  );
  program.showHelpAfterError();
  program.exitOverride();

  try {
    program.parse(argv, { from: 'user' });
  } catch (err) {
    if (err.code === 'commander.helpDisplayed' || err.code === 'commander.help') {
      result.help = true;
      return result;
    }
    throw err;
  }

  result.version = result.version || program.opts().version || null;
  result.force = result.force || Boolean(program.opts().force);

  return result;
}

module.exports = { parseArgs };

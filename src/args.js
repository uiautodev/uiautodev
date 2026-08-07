'use strict';

const USAGE = `Usage: uiautodev [options] [-- <server-args...>]

Download the UIAutoDev server binary and run it.

Options:
  --version <v>      Use a specific version (default: latest)
  --force            Force re-download even if cached
  --install-only     Download only, print the binary path and exit
  --help             Show this help

Anything after "--" is passed through to the server binary as-is.
`;

function parseArgs(argv) {
  const opts = {
    version: null,
    force: false,
    installOnly: false,
    help: false,
    passthrough: [],
  };

  let afterDoubleDash = false;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (afterDoubleDash) {
      opts.passthrough.push(arg);
      continue;
    }
    if (arg === '--') {
      afterDoubleDash = true;
      continue;
    }
    if (arg === '--help' || arg === '-h') {
      opts.help = true;
      continue;
    }
    if (arg === '--force' || arg === '-f') {
      opts.force = true;
      continue;
    }
    if (arg === '--install-only') {
      opts.installOnly = true;
      continue;
    }
    if (arg === '--version' || arg === '-v') {
      const value = argv[i + 1];
      if (value === undefined || value.startsWith('-')) {
        throw new Error('--version requires a value, e.g. --version 0.6.0');
      }
      opts.version = value;
      i += 1;
      continue;
    }
    if (arg.startsWith('--version=')) {
      opts.version = arg.slice('--version='.length);
      continue;
    }
    opts.passthrough.push(arg);
  }

  return opts;
}

module.exports = { parseArgs, USAGE };

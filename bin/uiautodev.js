#!/usr/bin/env node
'use strict';

const { run } = require('../src/index.js');

run(process.argv.slice(2)).catch((err) => {
  process.stderr.write(`uiautodev: ${err.message}\n`);
  process.exitCode = 1;
});

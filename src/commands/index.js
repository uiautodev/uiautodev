'use strict';

const run = require('./run');
const download = require('./download');
const pathCommand = require('./path');

module.exports = {
  run,
  download,
  path: pathCommand,
};

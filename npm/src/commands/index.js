'use strict';

const run = require('./run');
const download = require('./download');
const pathCommand = require('./path');
const create = require('./create');

module.exports = {
  run,
  download,
  path: pathCommand,
  create,
};

'use strict';

const debug = require('debug');

let enabledByFlag = false;

function createLogger(namespace) {
  return debug(`uiautodev:${namespace}`);
}

function enableDebug() {
  enabledByFlag = true;
  debug.enable(
    process.env.DEBUG ? `${process.env.DEBUG},uiautodev:*` : 'uiautodev:*'
  );
}

function isDebugEnabled() {
  return (
    enabledByFlag ||
    Boolean(process.env.DEBUG && /uiautodev/.test(process.env.DEBUG))
  );
}

module.exports = { createLogger, enableDebug, isDebugEnabled };

'use strict';

const SAFE_PATH = /^[A-Za-z0-9_@%+=:,./-]+$/;

function quotePath(value, platform = process.platform) {
  const str = String(value);
  if (SAFE_PATH.test(str)) return str;
  if (platform === 'win32') {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return `'${str.replace(/'/g, `'\\''`)}'`;
}

module.exports = { quotePath };

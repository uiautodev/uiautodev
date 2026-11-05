'use strict';

const { test } = require('node:test');
const assert = require('node:assert/strict');

const { resolveBinary } = require('../src/platform.js');

function files(...names) {
  return names.map((name) => ({
    name,
    size: 100,
    download_url: `https://dl.uiauto.dev/v/${name}`,
  }));
}

const V = '0.6.0';
const sample = files(
  '0.6.0',
  'UiautodevDesktop-mac-0.6.0.dmg',
  'uiautodev-server-darwin-amd64-0.6.0',
  'uiautodev-server-linux-arm64-0.6.0',
  'uiautodev-server-linux-x86_64-0.6.0',
  'uiautodev-server-windows-amd64-0.6.0.exe'
);

test('darwin x64 matches darwin-amd64', () => {
  const m = resolveBinary({ platform: 'darwin', arch: 'x64', files: sample });
  assert.equal(m.name, 'uiautodev-server-darwin-amd64-0.6.0');
});

test('darwin arm64 falls back to amd64 when no arm64 build', () => {
  const m = resolveBinary({ platform: 'darwin', arch: 'arm64', files: sample });
  assert.equal(m.name, 'uiautodev-server-darwin-amd64-0.6.0');
});

test('darwin arm64 prefers arm64 build when present', () => {
  const fs2 = [...sample, { name: 'uiautodev-server-darwin-arm64-0.6.0', size: 100, download_url: 'x' }];
  const m = resolveBinary({ platform: 'darwin', arch: 'arm64', files: fs2 });
  assert.equal(m.name, 'uiautodev-server-darwin-arm64-0.6.0');
});

test('accepts future darwin-x86_64 naming', () => {
  const fs2 = files('uiautodev-server-darwin-x86_64-0.6.0');
  const m = resolveBinary({ platform: 'darwin', arch: 'x64', files: fs2 });
  assert.equal(m.name, 'uiautodev-server-darwin-x86_64-0.6.0');
});

test('linux x64 matches x86_64', () => {
  const m = resolveBinary({ platform: 'linux', arch: 'x64', files: sample });
  assert.equal(m.name, 'uiautodev-server-linux-x86_64-0.6.0');
});

test('linux arm64 matches arm64', () => {
  const m = resolveBinary({ platform: 'linux', arch: 'arm64', files: sample });
  assert.equal(m.name, 'uiautodev-server-linux-arm64-0.6.0');
});

test('win32 x64 matches .exe', () => {
  const m = resolveBinary({ platform: 'win32', arch: 'x64', files: sample });
  assert.equal(m.name, 'uiautodev-server-windows-amd64-0.6.0.exe');
});

test('size-0 and non-server entries are ignored', () => {
  const zero = {
    name: 'uiautodev-server-darwin-amd64-0.6.0',
    size: 0,
    download_url: 'x',
  };
  const m = resolveBinary({
    platform: 'darwin',
    arch: 'x64',
    files: [...sample, zero],
  });
  assert.equal(m.name, 'uiautodev-server-darwin-amd64-0.6.0');
});

test('unsupported platform returns null', () => {
  const m = resolveBinary({ platform: 'freebsd', arch: 'x64', files: sample });
  assert.equal(m, null);
});

test('no match returns null', () => {
  const m = resolveBinary({
    platform: 'linux',
    arch: 'arm64',
    files: files('uiautodev-server-linux-x86_64-0.6.0'),
  });
  assert.equal(m, null);
});

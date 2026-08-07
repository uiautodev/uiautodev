'use strict';

const OS_MAP = {
  darwin: 'macOS',
  linux: 'Linux',
  win32: 'Windows',
};

function buildServerPatterns(platform, arch) {
  const arch64 = '(amd64|x86_64)';
  switch (platform) {
    case 'darwin':
      if (arch === 'arm64') {
        return [
          /uiautodev-server-darwin-arm64/i,
          new RegExp(`uiautodev-server-darwin-${arch64}`, 'i'),
        ];
      }
      return [new RegExp(`uiautodev-server-darwin-${arch64}`, 'i')];
    case 'linux':
      if (arch === 'arm64') {
        return [/uiautodev-server-linux-arm64/i];
      }
      return [new RegExp(`uiautodev-server-linux-${arch64}`, 'i')];
    case 'win32':
      if (arch === 'x64') {
        return [/uiautodev-server-windows-(amd64|x86_64).*\.exe/i];
      }
      return [];
    default:
      return [];
  }
}

function resolveBinary({ platform, arch, files }) {
  const patterns = buildServerPatterns(platform, arch);
  const candidates = files.filter(
    (f) => f && f.size > 0 && f.download_url && /uiautodev-server/i.test(f.name)
  );
  for (const pattern of patterns) {
    const match = candidates.find((f) => pattern.test(f.name));
    if (match) return match;
  }
  return null;
}

function describePlatform(platform, arch) {
  const os = OS_MAP[platform] || platform;
  return `${os} (${arch})`;
}

module.exports = { resolveBinary, buildServerPatterns, describePlatform };

'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const https = require('node:https');
const { spawn } = require('node:child_process');

const { parseArgs } = require('./args');
const { resolveBinary, describePlatform } = require('./platform');
const { createLogger, enableDebug, isDebugEnabled } = require('./log');

const mainLog = createLogger('main');
const postLog = createLogger('post');

const API_BASE = 'https://download.devsleep.com';
const DOWNLOAD_BASE = 'https://dl.uiauto.dev';

const REQUEST_HEADERS = {
  'user-agent': 'uiautodev-cli',
};

function request(url, redirects = 5) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: REQUEST_HEADERS }, (res) => {
      const { statusCode, headers } = res;
      if (statusCode >= 300 && statusCode < 400 && headers.location && redirects > 0) {
        res.resume();
        resolve(request(new URL(headers.location, url).toString(), redirects - 1));
        return;
      }
      resolve(res);
    });
    req.on('error', reject);
  });
}

async function getJson(url) {
  const res = await request(url);
  const chunks = [];
  for await (const chunk of res) chunks.push(chunk);
  const body = Buffer.concat(chunks).toString('utf8');
  if (res.statusCode >= 400) {
    throw new Error(`HTTP ${res.statusCode} for ${url}: ${body.slice(0, 200)}`);
  }
  return JSON.parse(body);
}

function downloadStatUrl(version, fileName) {
  return `${API_BASE}/api/versions/${encodeURIComponent(
    version
  )}/files/${encodeURIComponent(fileName)}/downloads`;
}

function postDownloadStat(version, fileName) {
  const url = downloadStatUrl(version, fileName);
  postLog('POST %s', url);
  return new Promise((resolve) => {
    let settled = false;
    const finish = () => {
      if (!settled) {
        settled = true;
        resolve();
      }
    };
    const req = https.request(
      url,
      { method: 'POST', headers: REQUEST_HEADERS },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const body = Buffer.concat(chunks).toString('utf8');
          postLog(
            'POST %s -> %s%s',
            url,
            res.statusCode,
            body ? ` ${body}` : ''
          );
          finish();
        });
      }
    );
    req.setTimeout(2000, () => {
      postLog('POST %s timed out', url);
      req.destroy();
      finish();
    });
    req.on('error', (err) => {
      postLog('POST %s error: %s', url, err.message);
      finish();
    });
    req.end();
  });
}

async function getLatestVersion() {
  const data = await getJson(`${API_BASE}/api/versions`);
  if (!Array.isArray(data.versions) || data.versions.length === 0) {
    throw new Error('No versions available from the download server');
  }
  return data.versions[0];
}

async function getVersionFiles(version) {
  const data = await getJson(
    `${API_BASE}/api/versions/${encodeURIComponent(version)}`
  );
  if (!Array.isArray(data.files) || data.files.length === 0) {
    throw new Error(`No files found for version ${version}`);
  }
  return { version: data.version || version, files: data.files };
}

function getCacheDir(version) {
  const base =
    process.env.UIAUTODEV_CACHE_DIR ||
    path.join(os.homedir(), '.cache', 'uiautodev');
  return path.join(base, version);
}

async function downloadTo(url, destPath, expectedSize) {
  const tmpPath = `${destPath}.tmp-${process.pid}-${Date.now()}`;
  const res = await request(url);
  if (res.statusCode >= 400) {
    res.resume();
    throw new Error(`HTTP ${res.statusCode} for ${url}`);
  }
  await new Promise((resolve, reject) => {
    const fileStream = fs.createWriteStream(tmpPath);
    const progress = new ProgressBar(expectedSize);
    let received = 0;
    res.on('data', (chunk) => {
      received += chunk.length;
      progress.render(received);
    });
    res.on('error', (err) => {
      progress.clear();
      fileStream.destroy();
      reject(err);
    });
    fileStream.on('finish', () => {
      progress.finish(received);
      resolve();
    });
    fileStream.on('error', (err) => {
      progress.clear();
      reject(err);
    });
    res.pipe(fileStream);
  });

  const size = fs.statSync(tmpPath).size;
  if (expectedSize != null && size !== expectedSize) {
    fs.unlinkSync(tmpPath);
    throw new Error(
      `Size mismatch for ${url}: expected ${expectedSize} bytes, got ${size} bytes`
    );
  }
  if (fs.existsSync(destPath)) fs.unlinkSync(destPath);
  fs.renameSync(tmpPath, destPath);
}

const CLEAR_WIDTH = 90;

class ProgressBar {
  constructor(total) {
    this.total = total || 0;
    this.width = 20;
    this.lastRender = 0;
    this.lastBytes = 0;
    this.lastTime = Date.now();
    this.speed = 0;
  }

  render(received) {
    const now = Date.now();
    if (now - this.lastRender < 100) return;

    const elapsed = (now - this.lastTime) / 1000;
    if (elapsed > 0) {
      const instSpeed = (received - this.lastBytes) / elapsed;
      this.speed = this.speed === 0 ? instSpeed : this.speed * 0.8 + instSpeed * 0.2;
    }
    this.lastRender = now;
    this.lastBytes = received;
    this.lastTime = now;

    const pct =
      this.total > 0 ? Math.min(100, (received / this.total) * 100) : 0;
    const filled = Math.round((pct / 100) * this.width);
    const bar = '#'.repeat(filled) + '-'.repeat(this.width - filled);
    const recvPadded = formatBytes(received).padStart(9);
    const totalPadded = this.total > 0 ? formatBytes(this.total).padStart(9) : '';
    const sizeStr = this.total > 0 ? `${recvPadded} / ${totalPadded}` : recvPadded;
    const pctStr = `${String(Math.round(pct)).padStart(3)}%`;
    const speed = this.speed > 0 ? `${formatBytes(this.speed)}/s` : '';
    const speedStr = speed.padEnd(10);
    const etaStr =
      this.total > 0 && this.speed > 0
        ? formatDuration((this.total - received) / this.speed)
        : '';
    const line = `Downloading ${sizeStr} [${bar}] ${pctStr} ${speedStr} ETA ${etaStr.padStart(5)}`;
    process.stderr.write(`\r${line}`);
  }

  finish(received) {
    process.stderr.write(`\r${' '.repeat(CLEAR_WIDTH)}\r`);
    if (this.total > 0) {
      process.stderr.write(`Downloaded ${formatBytes(received)} (100%)\n`);
    } else {
      process.stderr.write(`Downloaded ${formatBytes(received)}\n`);
    }
  }

  clear() {
    process.stderr.write(`\r${' '.repeat(CLEAR_WIDTH)}\r`);
  }
}

function formatDuration(seconds) {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const sec = s % 60;
  return `${m}m${String(sec).padStart(2, '0')}s`;
}

function formatBytes(n) {
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = n;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i += 1;
  }
  const digits = value >= 100 || i === 0 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[i]}`;
}

async function run(argv) {
  const opts = parseArgs(argv);

  if (opts.debug) {
    enableDebug();
  }

  if (opts.help) {
    return;
  }

  const version = opts.version || (await getLatestVersion());
  const { files } = await getVersionFiles(version);

  const binary = resolveBinary({
    platform: process.platform,
    arch: process.arch,
    files,
  });
  if (!binary) {
    throw new Error(
      `No server binary available for ${describePlatform(
        process.platform,
        process.arch
      )} in version ${version}`
    );
  }

  const dir = getCacheDir(version);
  const binPath = path.join(dir, binary.name);

  mainLog('resolved version=%s binary=%s', version, binary.name);

  if (opts.command === 'path') {
    console.log(binPath);
    return;
  }

  fs.mkdirSync(dir, { recursive: true });

  if (opts.force || !fs.existsSync(binPath)) {
    process.stderr.write(
      `Downloading ${binary.name} (${formatBytes(binary.size)})...\n`
    );
    await downloadTo(binary.download_url, binPath, binary.size);
    await postDownloadStat(version, binary.name);
  }

  if (process.platform !== 'win32') {
    fs.chmodSync(binPath, 0o755);
  }

  if (opts.command === 'download') {
    console.log(binPath);
    return;
  }

  await new Promise((resolve) => {
    const proc = spawn(binPath, opts.passthrough, { stdio: 'inherit' });
    for (const sig of ['SIGINT', 'SIGTERM']) {
      process.on(sig, () => proc.kill(sig));
    }
    proc.on('error', (err) => {
      process.stderr.write(`uiautodev: failed to start server: ${err.message}\n`);
      process.exitCode = 1;
      resolve();
    });
    proc.on('exit', (code, signal) => {
      if (signal) process.exitCode = 1;
      else if (code != null) process.exitCode = code;
      resolve();
    });
  });
}

module.exports = {
  run,
  getLatestVersion,
  getVersionFiles,
  getCacheDir,
  downloadTo,
  downloadStatUrl,
  postDownloadStat,
  formatBytes,
  API_BASE,
  DOWNLOAD_BASE,
};

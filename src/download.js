'use strict';

const fs = require('node:fs');
const path = require('node:path');

const { request, postDownloadStat } = require('./api');
const { getCacheDir } = require('./cache');

const CLEAR_WIDTH = 90;

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

async function ensureBinary({ binary, version, force }) {
  const dir = getCacheDir(version);
  const binPath = path.join(dir, binary.name);

  fs.mkdirSync(dir, { recursive: true });

  if (force || !fs.existsSync(binPath)) {
    process.stderr.write(
      `Downloading ${binary.name} (${formatBytes(binary.size)})...\n`
    );
    postDownloadStat(version, binary.name);
    await downloadTo(binary.download_url, binPath, binary.size);
  }

  if (process.platform !== 'win32') {
    fs.chmodSync(binPath, 0o755);
  }

  return binPath;
}

module.exports = {
  downloadTo,
  ensureBinary,
  ProgressBar,
  formatBytes,
  formatDuration,
};

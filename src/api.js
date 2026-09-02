'use strict';

const https = require('node:https');

const { createLogger } = require('./log');

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
    req.on('socket', (socket) => socket.unref());
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

module.exports = {
  request,
  getJson,
  getLatestVersion,
  getVersionFiles,
  downloadStatUrl,
  postDownloadStat,
  API_BASE,
  DOWNLOAD_BASE,
};

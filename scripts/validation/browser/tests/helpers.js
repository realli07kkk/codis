'use strict';

// Shared helpers for the Codis browser validation suite.
//
// The FE is an AngularJS single page app served by cmd/fe. Product selection
// lives in the URL hash (#/<product>/<page>); the ?forward=<product> query is
// only read by the codis-fe reverse proxy when the page calls the dashboard
// API. It is not persisted in localStorage, so openFE() sets both.

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const { execFileSync } = require('child_process');
const { expect } = require('@playwright/test');

const REPO_ROOT = path.resolve(__dirname, '../../../..');
const TERMINAL_PITR_STATES = ['succeeded', 'failed', 'cancelled'];
const DEFAULT_PRODUCT = 'codis-validation';
const DEFAULT_BASE_URL = 'http://127.0.0.1:8080';
const DEFAULT_DASHBOARD = '127.0.0.1:18080';
const RDB_SEED_KEY = 'codis-browser-validation:rdb-seed';

let cachedContext = null;

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function sha256(value) {
  return crypto.createHash('sha256').update(value, 'utf8').digest('hex');
}

function isPresent(value) {
  return value !== undefined && value !== null && String(value).trim() !== '';
}

function splitAddr(addr) {
  const text = String(addr || '');
  if (text.startsWith('[')) {
    const end = text.indexOf(']');
    return { host: text.slice(1, end), port: Number(text.slice(end + 2)) };
  }
  const index = text.lastIndexOf(':');
  return { host: text.slice(0, index), port: Number(text.slice(index + 1)) };
}

function groupServers(cluster) {
  const servers = [];
  for (const group of (cluster && cluster.groups) || []) {
    for (const server of group.servers || []) {
      servers.push(Object.assign({ gid: group.gid }, server));
    }
  }
  return servers;
}

function firstMasterServer(cluster) {
  const servers = groupServers(cluster);
  return servers.find((server) => server.role === 'master') || servers[0] || null;
}

function serverAddr(server) {
  if (!server) {
    return null;
  }
  return server.addr || (server.host && server.port ? `${server.host}:${server.port}` : null);
}

function firstAofMaster(cluster) {
  const servers = groupServers(cluster);
  const masters = servers.filter((server) => server.role === 'master');
  const candidate = masters.find((server) => server.aof === true) ||
        masters[0] ||
        servers.find((server) => server.aof === true) ||
        servers[0];
  return candidate ? candidate.addr : null;
}

function readDbfilename(server) {
  if (!server || !server.conf || !fs.existsSync(server.conf)) {
    return null;
  }
  const conf = fs.readFileSync(server.conf, 'utf8');
  const match = conf.match(/^\s*dbfilename\s+(.+?)\s*$/m);
  if (!match) {
    return null;
  }
  return match[1].replace(/^["']|["']$/g, '');
}

function runRedisCli(redisCli, addr, args) {
  const { host, port } = splitAddr(addr);
  if (!host || !Number.isInteger(port) || port <= 0) {
    throw new Error(`invalid Redis address ${JSON.stringify(addr)}`);
  }
  return execFileSync(redisCli, ['-h', host, '-p', String(port), '--raw', ...args], {
    encoding: 'utf8',
    timeout: 30 * 1000,
  });
}

function resolveRdbFile(cluster, redisCli) {
  if (isPresent(process.env.CODIS_RDB_FILE)) {
    const file = path.resolve(process.env.CODIS_RDB_FILE);
    if (!fs.existsSync(file)) {
      throw new Error(`CODIS_RDB_FILE does not exist: ${file}`);
    }
    return file;
  }
  if (!cluster) {
    return null;
  }
  const master = firstMasterServer(cluster);
  if (!master) {
    return null;
  }
  const dir = master.dir;
  const dbfilename = readDbfilename(master);
  if (!dir || !dbfilename) {
    return null;
  }
  const file = path.resolve(dir, dbfilename);
  if (!fs.existsSync(file)) {
    const addr = serverAddr(master);
    try {
      // setup_cluster.py writes `save ""`, so the first worker to resolve the
      // default RDB file also makes sure the snapshot has at least one object.
      runRedisCli(redisCli, addr, ['SET', RDB_SEED_KEY, String(Date.now())]);
      runRedisCli(redisCli, addr, ['SAVE']);
      runRedisCli(redisCli, addr, ['DEL', RDB_SEED_KEY]);
    } catch (err) {
      throw new Error(
        `RDB file ${file} is missing and redis-cli SAVE on ${addr} failed: ${err.message}`
      );
    }
  }
  if (!fs.existsSync(file)) {
    throw new Error(`RDB file ${file} is still missing after redis-cli SAVE`);
  }
  return file;
}

function resolveXauth(cluster, product) {
  if (isPresent(process.env.CODIS_XAUTH)) {
    return process.env.CODIS_XAUTH.trim();
  }
  if (cluster && cluster.xauth && cluster.product === product) {
    return cluster.xauth;
  }
  return sha256(`Codis-XAuth-[${product}]`).slice(0, 32);
}

function loadContext() {
  if (cachedContext) {
    return cachedContext;
  }

  let cluster = null;
  if (isPresent(process.env.CODIS_CLUSTER)) {
    const file = path.resolve(process.env.CODIS_CLUSTER);
    if (!fs.existsSync(file)) {
      throw new Error(`CODIS_CLUSTER not found: ${file}`);
    }
    cluster = JSON.parse(fs.readFileSync(file, 'utf8'));
  }

  const repo = path.resolve(process.env.CODIS_REPO || (cluster && cluster.repo) || REPO_ROOT);
  const product = process.env.CODIS_PRODUCT || (cluster && cluster.product) || DEFAULT_PRODUCT;
  const baseURL = process.env.CODIS_FE_URL || (cluster && cluster.fe && cluster.fe.url) || DEFAULT_BASE_URL;
  const dashboard = process.env.CODIS_DASHBOARD_ADDR ||
        (cluster && cluster.dashboard && cluster.dashboard.addr) ||
        DEFAULT_DASHBOARD;
  const redisCli = process.env.REDIS_CLI || path.join(repo, 'bin', 'redis-cli');
  const xauth = resolveXauth(cluster, product);
  const pitrMaster = process.env.CODIS_PITR_MASTER ||
        (cluster ? firstAofMaster(cluster) : null);
  const rdbMaster = cluster ? serverAddr(firstMasterServer(cluster)) : null;

  const context = {
    baseURL: baseURL.replace(/\/+$/, ''),
    product,
    dashboard,
    xauth,
    cluster,
    repo,
    redisCli,
    rdbMaster,
    pitrMaster,
  };
  // Resolving the upload fixture may run `redis-cli SAVE` and seed a key.
  // Keep it lazy so read-only specs (fe-smoke, ACL, QPS, PITR) never touch
  // Redis just by loading the context.
  let rdbResolved = false;
  let rdbValue = null;
  Object.defineProperty(context, 'rdbFile', {
    enumerable: true,
    get() {
      if (!rdbResolved) {
        rdbValue = resolveRdbFile(cluster, redisCli);
        rdbResolved = true;
      }
      return rdbValue;
    },
  });

  cachedContext = context;
  return cachedContext;
}

async function openFE(page, hashPath) {
  const ctx = loadContext();
  const suffix = hashPath ? (hashPath.startsWith('/') ? hashPath : `/${hashPath}`) : '';
  const url = `${ctx.baseURL}/?forward=${encodeURIComponent(ctx.product)}` +
        `#/${encodeURIComponent(ctx.product)}${suffix}`;
  await page.goto(url, { waitUntil: 'domcontentloaded' });
  await expect(page.locator('.app-frame')).toBeVisible();
  await page.waitForFunction(
    (product) => location.hash.startsWith(`#/${encodeURIComponent(product)}`) &&
            !!document.querySelector('.page-container'),
    ctx.product
  );
  return url;
}

async function parseResponse(response) {
  const text = await response.text();
  let data = text;
  if (text === '') {
    data = null;
  } else {
    try {
      data = JSON.parse(text);
    } catch (ignore) {
      data = text;
    }
  }
  return { status: response.status(), data, text };
}

async function api(request, method, apiPath, body) {
  const ctx = loadContext();
  const url = `http://${ctx.dashboard}${apiPath}`;
  const options = { method, headers: {} };
  if (body !== undefined) {
    options.headers['content-type'] = 'application/json';
    options.data = JSON.stringify(body);
  }
  const response = await request.fetch(url, options);
  return parseResponse(response);
}

async function feApi(request, apiPath) {
  const ctx = loadContext();
  const separator = apiPath.includes('?') ? '&' : '?';
  const url = `${ctx.baseURL}${apiPath}${separator}forward=${encodeURIComponent(ctx.product)}`;
  const response = await request.fetch(url, { method: 'GET' });
  return parseResponse(response);
}

function capturePageErrors(page) {
  const errors = [];
  const badResponses = [];
  const onConsole = (message) => {
    if (message.type() === 'error') {
      errors.push(`console: ${message.text()}`);
    }
  };
  const onPageError = (error) => {
    errors.push(`pageerror: ${error && error.message ? error.message : String(error)}`);
  };
  const onResponse = (response) => {
    const url = response.url();
    if (response.status() >= 400 && !/\/favicon\.ico(\?|$)/.test(url)) {
      badResponses.push(`${response.status()} ${url}`);
    }
  };
  page.on('console', onConsole);
  page.on('pageerror', onPageError);
  page.on('response', onResponse);
  return {
    errors,
    badResponses,
    detach() {
      page.off('console', onConsole);
      page.off('pageerror', onPageError);
      page.off('response', onResponse);
    },
  };
}

function redisCli(ctx, addr, ...args) {
  return runRedisCli(ctx.redisCli, addr, args).trim();
}

async function pollJob(apiFetch, apiPath, isTerminal, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || 120 * 1000);
  let last = null;
  while (Date.now() < deadline) {
    last = await apiFetch('GET', apiPath);
    if (isTerminal(last.data)) {
      return last.data;
    }
    await sleep(1000);
  }
  throw new Error(
    `pollJob timed out after ${timeoutMs || 120000}ms for ${apiPath}: ` +
        JSON.stringify(last)
  );
}

function drawer(page, title) {
  const locator = page.locator('.resource-drawer');
  return title ? locator.filter({ hasText: title }) : locator;
}

async function waitForDrawer(page, title) {
  const locator = drawer(page, title);
  await expect(locator).toBeVisible();
  return locator;
}

function confirmDialog(page) {
  return page.locator('.confirm-dialog');
}

async function acceptConfirm(page, expectedText) {
  const dialog = confirmDialog(page);
  await expect(dialog).toBeVisible();
  if (expectedText) {
    await expect(dialog).toContainText(expectedText);
  }
  await dialog.locator('.confirm-accept').click();
  await expect(dialog).toHaveCount(0);
}

function dataRow(page, text) {
  return page.locator('codis-data-table tbody tr').filter({ hasText: text }).first();
}

function isPitrTerminal(state) {
  return TERMINAL_PITR_STATES.includes(state);
}

module.exports = {
  TERMINAL_PITR_STATES,
  acceptConfirm,
  api,
  capturePageErrors,
  confirmDialog,
  dataRow,
  drawer,
  feApi,
  isPitrTerminal,
  loadContext,
  openFE,
  pollJob,
  redisCli,
  sleep,
  splitAddr,
  waitForDrawer,
};

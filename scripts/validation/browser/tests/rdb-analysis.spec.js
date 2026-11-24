'use strict';

const { test, expect } = require('@playwright/test');
const {
  acceptConfirm,
  api,
  confirmDialog,
  loadContext,
  openFE,
  pollJob,
  redisCli,
  waitForDrawer,
} = require('./helpers');

test.describe.configure({ mode: 'serial' });

test.setTimeout(180 * 1000);

const TERMINAL = ['done', 'error', 'canceled'];
const SEED_KEYS = [
  'codis-browser-validation:rdb-seed:1',
  'codis-browser-validation:rdb-seed:2',
];

let ctx;
const createdJobIds = new Set();

function analysisJobPath(id) {
  return `/api/topom/rdb-analysis/${ctx.xauth}/${encodeURIComponent(id)}`;
}

function removeJobPath(id) {
  return `/api/topom/rdb-analysis/remove/${ctx.xauth}/${encodeURIComponent(id)}`;
}

function firstNumber(text) {
  const match = String(text).replace(/,/g, '').match(/\d+/);
  return match ? Number(match[0]) : NaN;
}

function firstMasterAddr() {
  if (ctx.rdbMaster) {
    return ctx.rdbMaster;
  }
  if (ctx.pitrMaster) {
    return ctx.pitrMaster;
  }
  for (const group of (ctx.cluster && ctx.cluster.groups) || []) {
    for (const server of group.servers || []) {
      if (server.role === 'master') {
        return server.addr;
      }
    }
  }
  return null;
}

function seedLiveKeys() {
  const addr = firstMasterAddr();
  if (!addr) {
    return;
  }
  for (const key of SEED_KEYS) {
    redisCli(ctx, addr, 'SET', key, 'browser-rdb-seed');
  }
}

function clearSeedKeys() {
  const addr = firstMasterAddr();
  if (!addr) {
    return;
  }
  redisCli(ctx, addr, 'DEL', ...SEED_KEYS);
}

function saveDefaultRdb() {
  if (process.env.CODIS_RDB_FILE) {
    // An explicit fixture is authoritative; never overwrite it.
    return;
  }
  const addr = firstMasterAddr();
  if (!addr || !ctx.rdbFile) {
    return;
  }
  redisCli(ctx, addr, 'SAVE');
}

async function waitForAnalysisJob(request, id, timeoutMs) {
  return pollJob(
    async (method, path) => {
      const result = await api(request, method, path);
      if (result.status !== 200) {
        throw new Error(`GET ${path} -> ${result.status}: ${JSON.stringify(result.data)}`);
      }
      return result;
    },
    analysisJobPath(id),
    (job) => job && TERMINAL.includes(job.status),
    timeoutMs || 120 * 1000
  );
}

async function expectPositiveSummary(page, job) {
  const summary = page.locator('#analysis-result-panel-summary');
  await expect(summary).toBeVisible({ timeout: 20 * 1000 });

  const objectsRow = summary.locator('tbody tr').filter({ hasText: 'objects_read' }).first();
  await expect(objectsRow).toBeVisible();
  const objectsText = await objectsRow.innerText();
  expect(objectsText).not.toContain('—');
  expect(firstNumber(objectsText)).toBeGreaterThan(0);
  expect(firstNumber(objectsText)).toBe(Number(job.objects_read));

  const fileRow = summary.locator('tbody tr').filter({ hasText: 'file_size' }).first();
  await expect(fileRow).toBeVisible();
  const fileText = await fileRow.innerText();
  expect(fileText).not.toContain('—');
  expect(firstNumber(fileText)).toBeGreaterThan(0);
  expect(firstNumber(fileText)).toBe(Number(job.file_size));

  const objectsItem = page.locator('.analysis-page .key-value-item').filter({ hasText: '对象数量' });
  await expect(objectsItem).toBeVisible();
  expect(firstNumber(await objectsItem.innerText())).toBe(Number(job.objects_read));
}

async function clearJobThroughUi(page) {
  const clear = page.getByRole('button', { name: '清除任务' });
  if (await clear.count() === 0) {
    return false;
  }
  await clear.click();
  await expect(confirmDialog(page)).toContainText('清除 RDB 分析任务');
  await acceptConfirm(page, '清除 RDB 分析任务');
  await expect(page.locator('#analysis-result-panel-summary')).toHaveCount(0);
  return true;
}

// A successful 清除任务 must release ctrl.actionPending on its own: otherwise
// the page renders the empty state while the header button stays disabled
// until a full reload.
async function expectNewAnalysisUsable(page) {
  const newAnalysis = page.locator('.analysis-page .page-header-actions')
    .getByRole('button', { name: '新建分析' });
  await expect(newAnalysis).toBeEnabled({ timeout: 15 * 1000 });
  await newAnalysis.click();
  const editor = await waitForDrawer(page, '新建 RDB 分析');
  await expect(editor.locator('#rdb-source-panel-upload')).toBeVisible();
  await editor.locator('.drawer-close').click();
  await expect(page.locator('.resource-drawer')).toHaveCount(0);
}

test.beforeAll(async () => {
  ctx = loadContext();
});

test.afterAll(async ({ request }) => {
  for (const id of Array.from(createdJobIds)) {
    try {
      // Remove is allowed for running and terminal jobs; it also cancels the
      // underlying analysis. A missing id (already cleared in the UI) is 800.
      const result = await api(request, 'PUT', removeJobPath(id));
      if (result.status !== 200 && result.status !== 800) {
        console.warn(`failed to remove RDB analysis job ${id}: ${result.status} ${JSON.stringify(result.data)}`);
      }
      createdJobIds.delete(id);
    } catch (err) {
      console.warn(`failed to remove RDB analysis job ${id}: ${err.message}`);
    }
  }
  try {
    clearSeedKeys();
  } catch (err) {
    console.warn(`failed to delete RDB seed keys: ${err.message}`);
  }
});

test('RDB analysis: uploads an RDB file, reports objects/file size > 0 and clears the job', async ({ page, request }) => {
  test.skip(!ctx.rdbFile, 'CODIS_RDB_FILE is unset and no master RDB file could be discovered');

  seedLiveKeys();
  saveDefaultRdb();

  await openFE(page, '/analysis');
  await expect(page.locator('.analysis-page')).toBeVisible();
  await page.getByRole('button', { name: '新建分析' }).first().click();
  const editor = await waitForDrawer(page, '新建 RDB 分析');
  await expect(editor.locator('#rdb-source-panel-upload')).toBeVisible();
  await editor.locator('#rdb-upload-file').setInputFiles(ctx.rdbFile);
  await expect(editor).toContainText('已选择：');

  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === 'POST' &&
        response.url().includes('/api/topom/rdb-analysis/upload/')
  );
  await editor.getByRole('button', { name: '开始分析' }).click();
  const response = await responsePromise;
  const payload = await response.json();
  expect(response.status(), JSON.stringify(payload)).toBe(200);
  expect(payload.id).toBeTruthy();
  const jobId = String(payload.id);
  createdJobIds.add(jobId);

  const job = await waitForAnalysisJob(request, jobId);
  expect(job.status).toBe('done');
  expect(job.source.startsWith('upload:')).toBe(true);
  expect(Number(job.objects_read)).toBeGreaterThan(0);
  expect(Number(job.file_size)).toBeGreaterThan(0);

  await expectPositiveSummary(page, job);
  await expect(page.locator('.analysis-page .metric-card').filter({ hasText: '任务状态' }))
    .toContainText('done');

  const cleared = await clearJobThroughUi(page);
  expect(cleared, 'the analysis page should expose a 清除任务 button').toBe(true);
  await expect(page.locator('.analysis-page')).toContainText('还没有 RDB 分析任务');
  await expectNewAnalysisUsable(page);
  await expect.poll(async () => {
    const result = await api(request, 'GET', analysisJobPath(jobId));
    return result.status;
  }, { timeout: 15 * 1000 }).toBe(800);
  createdJobIds.delete(jobId);
});

test('RDB analysis: remote fetch from a cluster master, reports objects/file size > 0 and clears the job', async ({ page, request }) => {
  test.skip(
    ctx.cluster && ctx.cluster.features && ctx.cluster.features.rdb_export === false,
    'RDB HTTP export / remote fetch is disabled in cluster.json features.rdb_export'
  );
  const remoteAddr = firstMasterAddr();
  test.skip(!remoteAddr, 'no Group Master address is available for remote fetch');

  seedLiveKeys();

  await openFE(page, '/analysis');
  await expect(page.locator('.analysis-page')).toBeVisible();
  await page.getByRole('button', { name: '新建分析' }).first().click();
  const editor = await waitForDrawer(page, '新建 RDB 分析');
  await editor.locator('#rdb-source-tab-remote').click();
  await expect(editor.locator('#rdb-source-panel-remote')).toBeVisible();

  await expect.poll(async () => editor.locator('#rdb-remote-options option').count(), {
    timeout: 15 * 1000,
  }).toBeGreaterThan(2);
  const labels = await editor.locator('#rdb-remote-options option').allInnerTexts();
  const label = labels.find((text) => text.includes(remoteAddr) && text.includes('Master')) ||
        labels.find((text) => text.includes(remoteAddr));
  if (label) {
    await editor.locator('#rdb-remote-options').selectOption({ label });
  } else {
    await editor.locator('#rdb-remote-server').fill(remoteAddr);
  }
  await expect(editor.locator('#rdb-remote-server')).toHaveValue(remoteAddr);

  const responsePromise = page.waitForResponse((response) =>
    response.request().method() === 'PUT' &&
        response.url().includes('/api/topom/rdb-analysis/remote-fetch/')
  );
  await editor.getByRole('button', { name: '开始分析' }).click();
  const response = await responsePromise;
  const payload = await response.json();
  if (response.status() === 800 && JSON.stringify(payload).includes('remote fetch is disabled')) {
    test.skip(true, `dashboard reports: ${payload.Cause || JSON.stringify(payload)}`);
  }
  expect(response.status(), JSON.stringify(payload)).toBe(200);
  expect(payload.id).toBeTruthy();
  const jobId = String(payload.id);
  createdJobIds.add(jobId);

  const job = await waitForAnalysisJob(request, jobId);
  expect(job.status).toBe('done');
  expect(job.source.startsWith('remote-http:')).toBe(true);
  expect(Number(job.objects_read)).toBeGreaterThan(0);
  expect(Number(job.file_size)).toBeGreaterThan(0);

  await expectPositiveSummary(page, job);
  await expect(page.locator('.analysis-page .metric-card').filter({ hasText: '任务状态' }))
    .toContainText('done');

  const cleared = await clearJobThroughUi(page);
  if (!cleared) {
    // The spec is still valid: afterAll removes the job through the dashboard
    // API when this FE build does not expose a clear/remove button.
    test.info().annotations.push({
      type: 'note',
      description: 'FE has no 清除任务 button; cleanup relies on the dashboard API',
    });
  } else {
    await expect(page.locator('.analysis-page')).toContainText('还没有 RDB 分析任务');
    await expectNewAnalysisUsable(page);
    await expect.poll(async () => {
      const result = await api(request, 'GET', analysisJobPath(jobId));
      return result.status;
    }, { timeout: 15 * 1000 }).toBe(800);
    createdJobIds.delete(jobId);
  }
});

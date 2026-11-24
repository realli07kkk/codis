'use strict';

const { test, expect } = require('@playwright/test');
const { capturePageErrors, loadContext, openFE } = require('./helpers');

test.describe.configure({ mode: 'serial' });

const PAGES = [
  {
    name: 'overview',
    hash: '',
    container: '.overview-page',
    readyText: '资源健康',
  },
  {
    name: 'security',
    hash: '/security',
    container: '.security-page',
    readyText: 'ACL 用户',
  },
  {
    name: 'analysis',
    hash: '/analysis',
    container: '.analysis-page',
    readyText: '还没有 RDB 分析任务',
  },
  {
    name: 'recovery',
    hash: '/recovery',
    container: '.recovery-page',
    readyText: '任务摘要',
  },
];

for (const pageDef of PAGES) {
  test(`FE smoke: ${pageDef.name} page loads without console or HTTP errors`, async ({ page }) => {
    const ctx = loadContext();
    const captured = capturePageErrors(page);
    try {
      await openFE(page, pageDef.hash);

      const container = page.locator(pageDef.container);
      await expect(container).toBeVisible();
      await expect(container).toContainText(pageDef.readyText);

      const expectedHash = pageDef.hash
        ? `#/${ctx.product}${pageDef.hash}`
        : `#/${ctx.product}`;
      expect(new URL(page.url()).hash).toBe(expectedHash);

      // Allow the page's background requests (cluster stats, ACL/qps list,
      // PITR job list) to settle before asserting the error collectors.
      await page.waitForTimeout(1000);

      expect(captured.errors, 'console errors / page errors').toEqual([]);
      expect(captured.badResponses, 'HTTP responses with status >= 400').toEqual([]);
    } finally {
      captured.detach();
    }
  });
}

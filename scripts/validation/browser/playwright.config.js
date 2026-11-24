'use strict';

const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: './tests',
  // All specs drive one live Codis cluster; running them in parallel would let
  // ACL/QPS/PITR/RDB mutations race each other.
  workers: 1,
  timeout: 90 * 1000,
  expect: {
    timeout: 15 * 1000,
  },
  retries: 0,
  reporter: [
    ['list'],
    ['json', { outputFile: 'test-results/results.json' }],
  ],
  use: {
    baseURL: process.env.CODIS_FE_URL || 'http://127.0.0.1:8080',
    headless: true,
    trace: 'off',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        headless: true,
      },
    },
  ],
});

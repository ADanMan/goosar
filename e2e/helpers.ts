import { expect, type Page } from '@playwright/test';
import { E2E_BASE_URL } from './base-url';
import { TestApiClient } from './fixtures';

const DEFAULT_E2E_NAME = 'E2E User';
const E2E_WORKER = process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? '0';
const E2E_RUN_ID =
  process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const DEFAULT_E2E_EMAIL = `e2e-${E2E_WORKER}-${E2E_RUN_ID}@goosar.ru`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-${E2E_WORKER}-${E2E_RUN_ID}`;

export async function useEnglishUi(page: Page): Promise<void> {
  await page.context().addCookies([{ name: 'goosar-locale', value: 'en', url: E2E_BASE_URL }]);
}

export async function enableFeatureFlags(
  page: Page,
  flags: Record<string, boolean>,
): Promise<void> {
  await page.route('**/api/config', async (route) => {
    const response = await route.fetch();
    const json = (await response.json()) as {
      feature_flags?: Record<string, boolean>;
    };
    json.feature_flags = { ...(json.feature_flags ?? {}), ...flags };
    await route.fulfill({ response, json });
  });
}

async function waitForIssuesPage(page: Page) {
  await waitForPageText(page, 'New Issue');
  await expect(page.getByRole('button', { name: 'New Issue' })).toBeVisible({
    timeout: 15000,
  });
}

export async function waitForPageText(page: Page, text: string, timeout = 30000) {
  await page.waitForFunction((expected) => document.body?.innerText.includes(expected), text, {
    timeout,
  });
}

export async function reloadAppPage(page: Page) {
  await page.reload({ waitUntil: 'domcontentloaded' });
  await waitForPageText(page, 'Issues');
}

export async function loginAsDefault(page: Page): Promise<string> {
  await useEnglishUi(page);
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  const workspace = await api.ensureWorkspace(`E2E Workspace ${E2E_WORKER}`, DEFAULT_E2E_WORKSPACE);
  await api.markUserOnboarded();

  const token = api.getToken();
  if (!token) {
    throw new Error('E2E login did not return an auth token');
  }

  await page.addInitScript((t) => {
    localStorage.setItem('goosar_token', t);
    localStorage.setItem('goosar:chat:isOpen', 'false');
  }, token);
  await page.goto(`/${workspace.slug}/issues`, { waitUntil: 'domcontentloaded' });
  await waitForIssuesPage(page);
  return workspace.slug;
}

export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace(`E2E Workspace ${E2E_WORKER}`, DEFAULT_E2E_WORKSPACE);
  await api.markUserOnboarded();
  return api;
}

export async function preferManualCreateMode(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem(
      'goosar_create_mode',
      JSON.stringify({ state: { lastMode: 'manual' }, version: 0 }),
    );
  });
  await reloadAppPage(page);
  await waitForIssuesPage(page);
}

export async function openWorkspaceMenu(page: Page) {
  const workspaceButton = page.getByRole('button', { name: /E2E Workspace/ }).first();
  await expect(workspaceButton).toBeVisible({ timeout: 15000 });
  await workspaceButton.click();
  await expect(page.locator('[class*="popover"]')).toBeVisible();
}

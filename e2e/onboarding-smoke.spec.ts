import { test, expect } from '@playwright/test';
import { TestApiClient } from './fixtures';
import { useEnglishUi, waitForPageText } from './helpers';

const EMAIL = `onboarding-v3-${Date.now()}@localhost`;
const SHOTS_DIR = '/tmp/onboarding-v3-shots';

test.use({ viewport: { width: 1440, height: 900 } });

test.beforeEach(async ({ page }) => {
  await useEnglishUi(page);
});

test('onboarding — welcome → about you (answer path)', async ({ page }) => {
  const api = new TestApiClient();
  await api.login(EMAIL, 'OBv3 Tester');
  const token = api.getToken();

  await page.addInitScript((t) => {
    localStorage.setItem('goosar_token', t);
  }, token);
  await page.goto('/onboarding', { waitUntil: 'domcontentloaded' });
  await waitForPageText(page, 'Continue on web');

  await expect(page.getByRole('button', { name: 'Continue on web' })).toBeVisible({
    timeout: 15000,
  });
  await page.screenshot({ path: `${SHOTS_DIR}/01-welcome.png`, fullPage: false });

  await page.getByRole('button', { name: 'Continue on web' }).click();

  await expect(page.getByText('Tell us a bit about you.')).toBeVisible({ timeout: 10000 });
  await expect(page.getByText('Which best describes you?')).toBeVisible();
  await expect(page.getByText('What do you want to use Goosar for?')).toBeVisible();
  await expect(page.getByText(/Step 1 of 4/)).toBeVisible();
  await expect(page.getByText('How did you hear about Goosar?')).toHaveCount(0);
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/02-about-you.png` });

  await page.getByRole('radio', { name: /Engineer \/ developer/i }).click();
  await page.getByRole('checkbox', { name: /Ship code with AI agents/i }).click();
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: /Name your workspace/i })).toBeVisible({
    timeout: 10000,
  });
  await expect(page.getByText(/Step 2 of 4/)).toBeVisible();
  await page.screenshot({ path: `${SHOTS_DIR}/03-workspace.png` });
});

test('onboarding — one skip clears the whole questionnaire step', async ({ page }) => {
  const api = new TestApiClient();
  await api.login(`skip-${Date.now()}@localhost`, 'Skipper');
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem('goosar_token', t), token);
  await page.goto('/onboarding', { waitUntil: 'domcontentloaded' });
  await waitForPageText(page, 'Continue on web');

  await page.getByRole('button', { name: 'Continue on web' }).click();
  await expect(page.getByText('Tell us a bit about you.')).toBeVisible({ timeout: 10000 });

  await page.getByRole('button', { name: 'Skip' }).click();
  await expect(page.getByRole('heading', { name: /Name your workspace/i })).toBeVisible({
    timeout: 10000,
  });
  await page.screenshot({ path: `${SHOTS_DIR}/04-after-skip.png` });
});

test('onboarding — zh-Hans renders Chinese labels', async ({ page, context }) => {
  await context.addCookies([
    { name: 'goosar-locale', value: 'zh-Hans', url: 'http://localhost:13442' },
  ]);
  const api = new TestApiClient();
  await api.login(`zh-${Date.now()}@localhost`, '中文用户');
  const token = api.getToken();

  await page.addInitScript((t) => localStorage.setItem('goosar_token', t), token);
  await page.goto('/onboarding', { waitUntil: 'domcontentloaded' });
  await waitForPageText(page, '在 web 端继续');

  await page
    .getByRole('button')
    .first()
    .click()
    .catch(() => {});

  await expect(page.getByText('简单介绍一下你自己。')).toBeVisible({ timeout: 10000 });
  await expect(page.getByText('你是什么角色？')).toBeVisible();
  await page.waitForTimeout(500);
  await page.screenshot({ path: `${SHOTS_DIR}/05-about-you-zh.png` });
});

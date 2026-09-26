import { test, expect } from '@playwright/test';
import { TestApiClient } from '../fixtures';

const SCHEMES = ['light', 'dark'] as const;

const WORKER = process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? '0';
const RUN_ID = process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const EMAIL = `e2e-visual-${WORKER}-${RUN_ID}@goosar.ru`;
const WORKSPACE_SLUG = `e2e-visual-${WORKER}-${RUN_ID}`;

test.describe('Visual regression', () => {
  for (const scheme of SCHEMES) {
    test(`baseline screens - ${scheme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });

      await page.goto('/', { waitUntil: 'networkidle' });
      // The landing hero terminal replays a scripted "typing" sequence even
      // under prefers-reduced-motion; give it time to reach its steady state
      // so consecutive screenshots are pixel-stable.
      await page.waitForTimeout(4000);
      await expect(page).toHaveScreenshot(`landing-${scheme}.png`);

      const api = new TestApiClient();
      await api.login(EMAIL, 'E2E Visual User');
      const workspace = await api.ensureWorkspace('E2E Visual Workspace', WORKSPACE_SLUG);
      await api.markUserOnboarded();
      await api.createIssue('Visual regression issue');

      const token = api.getToken();
      await page.addInitScript((t) => {
        localStorage.setItem('goosar_token', t as string);
        localStorage.setItem('goosar:chat:isOpen', 'false');
      }, token);
      await page.goto(`/${workspace.slug}/issues`, { waitUntil: 'domcontentloaded' });
      await page.waitForLoadState('networkidle');

      // A first-run product tour can cover the issues list; dismiss it if present.
      const skipTour = page.getByText('Мне это знакомо');
      if (await skipTour.count()) {
        await skipTour.first().click();
        await page.waitForLoadState('networkidle');
      }

      const firstIssueLink = page.locator('a[href*="/issues/"]').first();
      await expect(firstIssueLink).toBeVisible({ timeout: 15000 });
      await expect(page).toHaveScreenshot(`issues-${scheme}.png`);

      await firstIssueLink.click();
      await page.waitForURL(/\/issues\/[^/?]+/);
      await page.waitForLoadState('networkidle');
      await expect(page).toHaveScreenshot(`issue-${scheme}.png`);

      await api.cleanup();
    });
  }
});

import { test, expect } from '@playwright/test';
import { TestApiClient } from './fixtures';
import { loginAsDefault, useEnglishUi, waitForPageText } from './helpers';

test.use({ viewport: { width: 1440, height: 900 } });

test.beforeEach(async ({ page }) => {
  await useEnglishUi(page);
});

test('onboarding offers roles instead of creating a workspace', async ({ page }) => {
  const api = new TestApiClient();
  await api.login(`join-role-${Date.now()}@localhost`, 'Join Role Tester');
  const token = api.getToken();

  await page.route('**/api/deployment/join-targets', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: '00000000-0000-0000-0000-0000000000hr',
          slug: 'hr',
          name: 'HR',
          description: 'Hiring, onboarding, people operations',
          template_key: 'hr',
          member_count: 4,
        },
      ]),
    }),
  );

  await page.addInitScript((t) => {
    localStorage.setItem('goosar_token', t);
  }, token);
  await page.goto('/onboarding', { waitUntil: 'domcontentloaded' });
  await waitForPageText(page, 'Continue on web');

  await page.getByRole('button', { name: 'Continue on web' }).click();
  await page.getByRole('radio', { name: /Engineer \/ developer/i }).click();
  await page.getByRole('checkbox', { name: /Ship code with AI agents/i }).click();
  await page.getByRole('button', { name: 'Continue' }).click();

  await expect(page.getByRole('heading', { name: /Which role is yours\?/i })).toBeVisible({
    timeout: 15000,
  });
  await expect(page.getByText('HR')).toBeVisible();
  await expect(page.getByText('Hiring, onboarding, people operations')).toBeVisible();
  await expect(page.getByRole('heading', { name: /Name your workspace/i })).toHaveCount(0);
});

test('a joined role shows what it still needs', async ({ page }) => {
  const workspaceSlug = await loginAsDefault(page);

  await page.route('**/api/autopilots**', (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        total: 8,
        autopilots: Array.from({ length: 8 }, (_, i) => ({
          id: `00000000-0000-0000-0000-00000000000${i}`,
          workspace_id: 'ws',
          title: `Seeded autopilot ${i}`,
          description: null,
          assignee_type: 'agent',
          assignee_id: '00000000-0000-0000-0000-000000000000',
          status: 'paused',
          is_template: true,
          execution_mode: 'auto',
          issue_title_template: null,
          created_by_type: 'system',
          created_by_id: '00000000-0000-0000-0000-000000000000',
          last_run_at: null,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        })),
      }),
    });
  });

  await page.route('**/api/workspaces/*/capabilities', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        template_key: 'hr',
        role_name: 'HR',
        role_summary: 'People operations',
        capabilities: [],
        sample_tasks: [],
      }),
    }),
  );

  await page.goto(`/${workspaceSlug}/capabilities`, {
    waitUntil: 'domcontentloaded',
  });

  await expect(page.getByTestId('role-card-autopilots')).toContainText('Autopilots (8)', {
    timeout: 15000,
  });
  await expect(page.getByTestId('role-card-runtime')).toBeVisible();
});

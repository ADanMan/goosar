import { test, expect } from '@playwright/test';
import { enableFeatureFlags, loginAsDefault, waitForPageText } from './helpers';

test.describe('Settings', () => {
  test('updating workspace name reflects in sidebar immediately', async ({ page }) => {
    const workspaceSlug = await loginAsDefault(page);

    const sidebarName = page.getByRole('button', { name: /E2E Workspace/ }).first();
    const originalName =
      (await sidebarName.innerText()).split('\n').pop()?.trim() ?? 'E2E Workspace';

    await page.goto(`/${workspaceSlug}/settings?tab=workspace`, { waitUntil: 'domcontentloaded' });
    await waitForPageText(page, 'General');

    const nameInput = page.locator('input[type="text"]').first();
    await nameInput.clear();
    const newName = 'Renamed WS ' + Date.now();
    await nameInput.fill(newName);

    await expect(page.getByText('Workspace settings saved').first()).toBeVisible({
      timeout: 10000,
    });

    await expect(page.getByRole('button', { name: new RegExp(newName) }).first()).toBeVisible();

    await nameInput.clear();
    await nameInput.fill(originalName.trim());
    await expect(page.getByRole('button', { name: new RegExp(originalName) }).first()).toBeVisible({
      timeout: 10000,
    });
  });

  test('connecting a Composio toolkit shows a toast and refreshes the list', async ({ page }) => {
    const workspaceSlug = await loginAsDefault(page);
    const settingsUrl = `/${workspaceSlug}/settings?tab=integrations`;

    await enableFeatureFlags(page, { composio_mcp_apps: true });

    let connected = false;

    await page.route('**/api/integrations/composio/toolkits', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([{ slug: 'notion', name: 'Notion', connectable: true }]),
      }),
    );

    await page.route('**/api/integrations/composio/connections', (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          connected
            ? [
                {
                  id: 'conn-notion-1',
                  toolkit_slug: 'notion',
                  status: 'active',
                  connected_at: new Date().toISOString(),
                  last_used_at: null,
                },
              ]
            : [],
        ),
      });
    });

    await page.route('**/api/integrations/composio/connect/init', (route) => {
      connected = true;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          redirect_url: `/settings?tab=integrations&connected=notion`,
        }),
      });
    });

    await page.goto(settingsUrl, { waitUntil: 'domcontentloaded' });
    await waitForPageText(page, 'Composio');

    await page
      .getByTestId('integration-row-composio')
      .getByRole('button', { name: /^Configure$/ })
      .click();

    await page
      .getByRole('button', { name: /^Connect$/ })
      .first()
      .click();

    await expect(page.getByText('Connected').first()).toBeVisible({ timeout: 10000 });

    await expect(page.getByRole('button', { name: /Disconnect/ }).first()).toBeVisible({
      timeout: 10000,
    });
    await expect(page).not.toHaveURL(/connected=notion/);
  });
});

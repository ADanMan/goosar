import { test, expect } from '@playwright/test';
import { useEnglishUi, waitForPageText } from './helpers';

test.describe('Locale default (#207)', () => {
  test.use({
    locale: 'en-US',
    extraHTTPHeaders: { 'Accept-Language': 'en-US,en;q=0.9' },
  });

  test('an English browser with no stored choice gets the Russian login screen', async ({
    page,
  }) => {
    await page.goto('/login', { waitUntil: 'domcontentloaded' });

    await waitForPageText(page, 'Вход в Goosar');
    await expect(page.getByText('Вход в Goosar')).toBeVisible();
    await expect(page.getByText('Укажите email — пришлём код для входа')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Продолжить' })).toBeDisabled();

    await expect(page.getByText('Sign in to Goosar')).toHaveCount(0);

    await expect(page.locator('html')).toHaveAttribute('lang', 'ru-RU');
  });

  test('an explicit English choice still wins over the Russian default', async ({ page }) => {
    await useEnglishUi(page);
    await page.goto('/login', { waitUntil: 'domcontentloaded' });

    await waitForPageText(page, 'Sign in to Goosar');
    await expect(page.getByText('Вход в Goosar')).toHaveCount(0);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  });
});

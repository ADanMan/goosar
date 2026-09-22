import { defineConfig } from '@playwright/test';
import { E2E_BASE_URL } from './e2e/base-url';

// No `storageState` here on purpose (#207). Pinning `goosar-locale=en` for
// every project would have put the whole suite — the only layer that looks at
// the actually rendered UI — permanently on the far side of the new default,
// so nothing would notice if a fresh visitor stopped getting Russian. Specs
// that assert English strings ask for English themselves via `useEnglishUi`
// from ./e2e/helpers; locale-default.spec.ts deliberately asks for nothing.
export default defineConfig({
  testDir: './e2e',
  timeout: 60000,
  workers: 1,
  retries: 0,
  use: {
    baseURL: E2E_BASE_URL,
    headless: true,
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
  // Don't auto-start servers — they must be running already
  // This avoids complexity and port conflicts during testing
});

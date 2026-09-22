import './env';
import { resolveFrontendOrigin } from '../apps/web/config/runtime-urls';

const FALLBACK_BASE_URL = 'http://localhost:3000';

export const E2E_BASE_URL = resolveFrontendOrigin(
  [process.env.PLAYWRIGHT_BASE_URL, process.env.FRONTEND_ORIGIN],
  FALLBACK_BASE_URL,
);

import { describe, expect, it } from 'vitest';

import { isOfficialMarketingHost } from './public-host';

describe('isOfficialMarketingHost', () => {
  it.each(['goosar.ru', 'www.goosar.ru', 'GOOSAR.RU', 'goosar.ru.'])(
    'recognizes %s as an official marketing host',
    (host) => {
      expect(isOfficialMarketingHost(host)).toBe(true);
    },
  );

  it.each(['app.goosar.ru', 'api.goosar.ru', 'localhost', 'goosar.test'])(
    'does not treat %s as the public marketing host',
    (host) => {
      expect(isOfficialMarketingHost(host)).toBe(false);
    },
  );
});

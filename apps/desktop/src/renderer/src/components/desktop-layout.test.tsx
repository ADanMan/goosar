// @vitest-environment jsdom

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

describe('desktop-layout header', () => {
  const source = readFileSync(join(__dirname, 'desktop-layout.tsx'), 'utf8');

  it('does not import KerberosTicketStatus', () => {
    expect(source).not.toContain('KerberosTicketStatus');
  });

  it('does not render the kerberos-ticket-status testid', () => {
    expect(source).not.toContain('kerberos-ticket-status');
  });
});

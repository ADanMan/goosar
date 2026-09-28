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

describe('desktop-layout SidebarTrigger controls the context panel (T-020)', () => {
  const source = readFileSync(join(__dirname, 'desktop-layout.tsx'), 'utf8');

  it('reads the shared context panel state', () => {
    expect(source).toContain('useContextPanelState');
  });

  it('wires SidebarProvider open/onOpenChange to that state, not an independent flag', () => {
    expect(source).toMatch(/open=\{!collapsed\}/);
    expect(source).toMatch(/onOpenChange=\{\(open\) => setCollapsed\(!open\)\}/);
  });
});

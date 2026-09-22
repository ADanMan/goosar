// @vitest-environment jsdom

import { describe, expect, it } from 'vitest';
import { kerberosTicketSummary } from './kerberos-ticket-status';

const HOUR = 3_600_000;
const NOW = Date.UTC(2026, 1, 3, 12, 0, 0);

describe('kerberosTicketSummary', () => {
  it('says nothing on a platform without in-app kinit', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: false,
          caBundlePresent: true,
          corpCaPresent: true,
          kerberosTicket: 'none',
          kerberosExpiresAt: null,
        },
        nowMs: NOW,
      }),
    ).toBeNull();
  });

  it('says nothing on a machine that is not provisioned for the perimeter', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: false,
          corpCaPresent: false,
          kerberosTicket: 'none',
          kerberosExpiresAt: null,
        },
        nowMs: NOW,
      }),
    ).toBeNull();
  });

  it('reports a missing ticket', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'none',
          kerberosExpiresAt: null,
        },
        nowMs: NOW,
      }),
    ).toEqual({ kind: 'none', hours: null, canRenew: false });
  });

  it('reports an expired ticket as a missing one — both need a new kinit', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'expired',
          kerberosExpiresAt: NOW - HOUR,
        },
        nowMs: NOW,
      }),
    ).toEqual({ kind: 'none', hours: null, canRenew: false });
  });

  it('reports hours left, rounded down, while the ticket is expiring', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'expiring_soon',
          kerberosExpiresAt: NOW + HOUR * 2.7,
        },
        nowMs: NOW,
      }),
    ).toEqual({ kind: 'expiring', hours: 2, canRenew: true });
  });

  it('drops the hour figure when less than an hour is left', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'expiring_soon',
          kerberosExpiresAt: NOW + HOUR * 0.4,
        },
        nowMs: NOW,
      }),
    ).toEqual({ kind: 'expiring', hours: null, canRenew: true });
  });

  it('reports a valid ticket with the hours it has left', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'valid',
          kerberosExpiresAt: NOW + HOUR * 9,
        },
        nowMs: NOW,
      }),
    ).toEqual({ kind: 'valid', hours: 9, canRenew: true });
  });

  it('says nothing when the ticket state is unknown', () => {
    expect(
      kerberosTicketSummary({
        state: {
          kerberosSupported: true,
          caBundlePresent: true,
          corpCaPresent: false,
          kerberosTicket: 'unknown',
          kerberosExpiresAt: null,
        },
        nowMs: NOW,
      }),
    ).toBeNull();
  });

  it('says nothing before the first perimeter state arrives', () => {
    expect(kerberosTicketSummary({ state: null, nowMs: NOW })).toBeNull();
  });
});

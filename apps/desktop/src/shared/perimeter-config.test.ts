import { describe, expect, it } from 'vitest';
import {
  aggregateLiveOverall,
  applyPerimeterExtras,
  buildPerimeterLiveStatus,
  evaluateCaCheck,
  evaluateDaemonCheck,
  evaluateKerberosCheck,
  evaluateLlmAuthCheck,
  evaluateProvisioningCheck,
  evaluateReachabilityCheck,
  DEFAULT_PERIMETER_EXTRAS,
  DEFAULT_PERIMETER_SETTINGS,
  deriveKinitPrincipal,
  deriveKinitRealm,
  deriveTicketAcceptance,
  deriveTicketSource,
  parseKlistCacheIdentity,
  parseKlistRenewUntil,
  isValidKerberosPrincipal,
  KERBEROS_EXPIRING_SOON_MS,
  llmApiBaseHost,
  mergeNoProxy,
  parseKlistTicket,
  parsePerimeterExtras,
  parsePerimeterSettings,
  perimeterProxyUrl,
  type DaemonReachability,
  type HttpReachability,
  type PerimeterApplyReport,
  type PerimeterCheck,
  type PerimeterLiveInputs,
  type PerimeterReadinessInputs,
  type ProvisioningReachability,
} from './perimeter-config';
import { applyRuntimeConfigPatch, parseRuntimeConfig } from './runtime-config';

describe('parsePerimeterSettings', () => {
  it('defaults to disabled with the stock px address when there is no document', () => {
    expect(parsePerimeterSettings(null)).toEqual({
      proxyHost: '127.0.0.1',
      proxyPort: 3128,
      noProxy: ['localhost', '127.0.0.1'],
    });
  });

  it('reads the operator overrides from the proxy block', () => {
    const raw = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: {
        perimeter: true,
        host: '10.0.0.5',
        port: 8888,
        noProxy: ['.example.test', '.other.example.test'],
      },
    });
    expect(parsePerimeterSettings(raw)).toEqual({
      proxyHost: '10.0.0.5',
      proxyPort: 8888,
      noProxy: ['localhost', '127.0.0.1', '.example.test', '.other.example.test'],
    });
  });

  it('accepts a comma-separated noProxy string', () => {
    const raw = JSON.stringify({
      proxy: { perimeter: true, noProxy: ' .example.test , internal.host ' },
    });
    expect(parsePerimeterSettings(raw).noProxy).toEqual([
      'localhost',
      '127.0.0.1',
      '.example.test',
      'internal.host',
    ]);
  });

  it('never lets an operator override drop the local bypass defaults', () => {
    const raw = JSON.stringify({
      proxy: { perimeter: true, noProxy: ['.corp.example'] },
    });
    expect(parsePerimeterSettings(raw).noProxy).toContain('127.0.0.1');
    expect(parsePerimeterSettings(raw).noProxy).toContain('localhost');
  });

  it('reads the legacy proxy.perimeter flag without letting it change anything', () => {
    const withFlag = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { perimeter: true, host: '10.0.0.5', port: 8888 },
    });
    const withoutFlag = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      proxy: { host: '10.0.0.5', port: 8888 },
    });

    expect(parsePerimeterSettings(withFlag)).toEqual(parsePerimeterSettings(withoutFlag));
    expect(Object.keys(parsePerimeterSettings(withFlag))).not.toContain('enabled');
  });

  it('falls back to defaults on mistyped overrides instead of failing boot', () => {
    const raw = JSON.stringify({
      proxy: {
        perimeter: true,
        host: 'http://not a host',
        port: '3128',
        noProxy: 42,
      },
    });
    expect(parsePerimeterSettings(raw)).toEqual({
      ...DEFAULT_PERIMETER_SETTINGS,
      noProxy: [...DEFAULT_PERIMETER_SETTINGS.noProxy],
    });
  });

  it('survives an unparsable document', () => {
    expect(parsePerimeterSettings('{nope')).toEqual(DEFAULT_PERIMETER_SETTINGS);
  });
});

describe('llmApiBaseHost (issue #146)', () => {
  it('extracts the bare host of an https api_base', () => {
    expect(llmApiBaseHost('https://llm.example.test/api/v3')).toBe('llm.example.test');
  });

  it('ignores port and path, returning only the host', () => {
    expect(llmApiBaseHost('http://gw.internal:8443/v1/chat')).toBe('gw.internal');
  });

  it('returns null for a non-URL, empty, or nullish value', () => {
    expect(llmApiBaseHost('not a url')).toBeNull();
    expect(llmApiBaseHost('')).toBeNull();
    expect(llmApiBaseHost('   ')).toBeNull();
    expect(llmApiBaseHost(null)).toBeNull();
    expect(llmApiBaseHost(undefined)).toBeNull();
  });
});

describe('mergeNoProxy (issue #146)', () => {
  it('appends extra entries, trimming and de-duplicating, never dropping the base', () => {
    expect(
      mergeNoProxy(['localhost', '127.0.0.1'], [' llm.example.test ', '127.0.0.1', '.internal']),
    ).toEqual(['localhost', '127.0.0.1', 'llm.example.test', '.internal']);
  });

  it('returns the base unchanged when there is nothing extra to add', () => {
    expect(mergeNoProxy(['localhost', '127.0.0.1'], [])).toEqual(['localhost', '127.0.0.1']);
  });
});

describe('perimeterProxyUrl', () => {
  it('builds the px URL from host and port', () => {
    expect(perimeterProxyUrl(DEFAULT_PERIMETER_SETTINGS)).toBe('http://127.0.0.1:3128');
  });
});

describe('deriveKinitRealm', () => {
  it('uppercases the email domain', () => {
    expect(deriveKinitRealm('user@example.test')).toBe('EXAMPLE.TEST');
  });

  it('returns null when there is no domain', () => {
    expect(deriveKinitRealm('user')).toBeNull();
    expect(deriveKinitRealm('user@')).toBeNull();
    expect(deriveKinitRealm('')).toBeNull();
  });
});

describe('deriveKinitPrincipal', () => {
  it('builds <local>@<REALM> from the email domain by default', () => {
    expect(deriveKinitPrincipal('user@example.test')).toBe('user@EXAMPLE.TEST');
  });

  it('prefers an explicit realm override, uppercased', () => {
    expect(deriveKinitPrincipal('user@example.test', 'corp.example.test')).toBe(
      'user@CORP.EXAMPLE.TEST',
    );
  });

  it('uses the whole string as the local part when a realm override is given without a domain', () => {
    expect(deriveKinitPrincipal('user', 'example.test')).toBe('user@EXAMPLE.TEST');
  });

  it('returns null for an unusable email or a missing realm', () => {
    expect(deriveKinitPrincipal('')).toBeNull();
    expect(deriveKinitPrincipal('user')).toBeNull();
    expect(deriveKinitPrincipal('has space@example.test')).toBeNull();
  });
});

describe('isValidKerberosPrincipal', () => {
  it('accepts primary@REALM and primary/instance@REALM', () => {
    expect(isValidKerberosPrincipal('user@EXAMPLE.TEST')).toBe(true);
    expect(isValidKerberosPrincipal('host/admin@EXAMPLE.TEST')).toBe(true);
  });

  it('rejects whitespace, missing realm, and injection-shaped input', () => {
    expect(isValidKerberosPrincipal('user')).toBe(false);
    expect(isValidKerberosPrincipal('user@')).toBe(false);
    expect(isValidKerberosPrincipal('d katalshov@EXAMPLE.TEST')).toBe(false);
    expect(isValidKerberosPrincipal('a@b@c')).toBe(false);
  });
});

describe('parsePerimeterExtras', () => {
  it('defaults everything to null when there is no perimeter block', () => {
    expect(parsePerimeterExtras(null)).toEqual(DEFAULT_PERIMETER_EXTRAS);
    expect(parsePerimeterExtras(JSON.stringify({ proxy: {} }))).toEqual(DEFAULT_PERIMETER_EXTRAS);
  });

  it('reads the realm, uppercased', () => {
    const raw = JSON.stringify({ perimeter: { realm: 'example.test' } });
    expect(parsePerimeterExtras(raw)).toEqual({
      realm: 'EXAMPLE.TEST',
      principalDomain: null,
      noProxy: [],
    });
  });

  it('reads perimeter.principalDomain, uppercased', () => {
    const raw = JSON.stringify({
      perimeter: { realm: 'example.com', principalDomain: ' example.net ' },
    });
    expect(parsePerimeterExtras(raw)).toEqual({
      realm: 'EXAMPLE.COM',
      principalDomain: 'EXAMPLE.NET',
      noProxy: [],
    });
  });

  it('ignores LLM profiles left in the block by an older install', () => {
    const raw = JSON.stringify({
      perimeter: {
        realm: 'example.test',
        llm: { apiBase: 'https://gw.internal/v3', model: 'm', apiKeyEnc: 'CIPHER' },
        llmBackup: { apiBase: 'https://outside/v1', model: 'm', apiKeyEnc: 'C2' },
      },
    });
    const extras = parsePerimeterExtras(raw);
    expect(extras).toEqual({
      realm: 'EXAMPLE.TEST',
      principalDomain: null,
      noProxy: [],
    });
    expect(JSON.stringify(extras)).not.toContain('CIPHER');
  });

  it('reads explicit internal noProxy suffixes from the perimeter block (issue #146)', () => {
    const raw = JSON.stringify({
      perimeter: { noProxy: [' .example.test ', 'gw.internal', '.example.test'] },
    });
    expect(parsePerimeterExtras(raw).noProxy).toEqual(['.example.test', 'gw.internal']);
  });

  it('accepts a comma-separated perimeter-block noProxy string', () => {
    const raw = JSON.stringify({
      perimeter: { noProxy: ' .example.test , gw.internal ' },
    });
    expect(parsePerimeterExtras(raw).noProxy).toEqual(['.example.test', 'gw.internal']);
  });

  it('survives an unparsable document', () => {
    expect(parsePerimeterExtras('{nope')).toEqual(DEFAULT_PERIMETER_EXTRAS);
  });
});

describe('applyPerimeterExtras', () => {
  it('writes the perimeter block without disturbing endpoints, proxy, or unmodelled fields', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      telemetryOptOut: true,
      proxy: { perimeter: true, host: '10.1.1.1' },
    });
    const result = applyPerimeterExtras(existing, { realm: 'example.test' });
    if (!result.ok) throw new Error(result.message);
    const doc = JSON.parse(result.json);
    expect(doc.apiUrl).toBe('https://goosar.ru');
    expect(doc.telemetryOptOut).toBe(true);
    expect(doc.proxy).toEqual({ perimeter: true, host: '10.1.1.1' });
    expect(doc.perimeter).toEqual({ realm: 'EXAMPLE.TEST' });
    expect(() => parseRuntimeConfig(result.json)).not.toThrow();
  });

  it('clears a field when the patch sets it to null, leaving absent fields alone', () => {
    const existing = JSON.stringify({
      schemaVersion: 1,
      apiUrl: 'https://goosar.ru',
      perimeter: { realm: 'EXAMPLE.TEST', noProxy: ['.corp.example'] },
    });
    const result = applyPerimeterExtras(existing, { realm: null });
    if (!result.ok) throw new Error(result.message);
    const block = JSON.parse(result.json).perimeter;
    expect(block.realm).toBeUndefined();
    expect(block.noProxy).toEqual(['.corp.example']);
  });

  it('refuses to replace a document it cannot parse', () => {
    expect(applyPerimeterExtras('{corrupt', { realm: 'X' }).ok).toBe(false);
  });
});

describe('parseKlistTicket (issue #148)', () => {
  const MONTH_ABBR = [
    'Jan',
    'Feb',
    'Mar',
    'Apr',
    'May',
    'Jun',
    'Jul',
    'Aug',
    'Sep',
    'Oct',
    'Nov',
    'Dec',
  ];
  function fmt(d: Date): string {
    const mon = MONTH_ABBR[d.getMonth()];
    const day = String(d.getDate()).padStart(2, ' ');
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    return `${mon} ${day} ${hh}:${mm}:${ss} ${d.getFullYear()}`;
  }
  function klistOutput(issued: Date, expires: Date): string {
    return [
      'Credentials cache: API:501:9',
      '        Principal: user@EXAMPLE.TEST',
      '',
      '  Issued                Expires               Principal',
      `${fmt(issued)}  ${fmt(expires)}  krbtgt/EXAMPLE.TEST@EXAMPLE.TEST`,
      '',
    ].join('\n');
  }

  const NOW = new Date('2026-08-24T10:00:00').getTime();

  it('reports valid with the parsed expiry when the tgt is comfortably ahead', () => {
    const expires = new Date(NOW + 8 * 3_600_000);
    const status = parseKlistTicket(klistOutput(new Date(NOW - 3_600_000), expires), NOW);
    expect(status.state).toBe('valid');
    expect(status.expiresAt).toBe(expires.getTime());
  });

  it('reports expiring_soon when the tgt is within the warn window', () => {
    const expires = new Date(NOW + 20 * 60_000); 
    const status = parseKlistTicket(klistOutput(new Date(NOW - 3_600_000), expires), NOW);
    expect(status.state).toBe('expiring_soon');
    expect(status.expiresAt).toBe(expires.getTime());
  });

  it('respects the exact expiring-soon boundary constant', () => {
    const expires = new Date(NOW + KERBEROS_EXPIRING_SOON_MS - 1_000);
    expect(parseKlistTicket(klistOutput(new Date(NOW), expires), NOW).state).toBe('expiring_soon');
    const stillValid = new Date(NOW + KERBEROS_EXPIRING_SOON_MS + 60_000);
    expect(parseKlistTicket(klistOutput(new Date(NOW), stillValid), NOW).state).toBe('valid');
  });

  it('reports expired when the tgt is in the past', () => {
    const expires = new Date(NOW - 5 * 60_000);
    const status = parseKlistTicket(klistOutput(new Date(NOW - 11 * 3_600_000), expires), NOW);
    expect(status.state).toBe('expired');
    expect(status.expiresAt).toBe(expires.getTime());
  });

  it('reports none for every Heimdal empty-cache wording', () => {
    const wordings = [
      'klist: krb5_cc_get_principal: No credentials cache file found (unknown)\n',
      'klist: Cache not found: API:12345678-1234-1234-1234-123456789abc\n',
      'klist: krb5_cc_resolve: failed to parse uuid: API:not-a-uuid\n',
      'klist: No ticket file: /tmp/krb5cc_501\n',
    ];
    for (const wording of wordings) {
      expect(parseKlistTicket(wording, NOW)).toEqual({
        state: 'none',
        expiresAt: null,
      });
    }
  });

  it('reports unknown for output it cannot interpret', () => {
    expect(parseKlistTicket('something unexpected entirely', NOW)).toEqual({
      state: 'unknown',
      expiresAt: null,
    });
    expect(parseKlistTicket('', NOW)).toEqual({
      state: 'unknown',
      expiresAt: null,
    });
  });

  it('uses the latest krbtgt expiry when several tgts are cached', () => {
    const early = new Date(NOW + 1 * 3_600_000);
    const late = new Date(NOW + 9 * 3_600_000);
    const output = [
      'Credentials cache: API:501:9',
      '        Principal: user@EXAMPLE.TEST',
      '',
      '  Issued                Expires               Principal',
      `${fmt(new Date(NOW))}  ${fmt(early)}  krbtgt/OTHER.RU@EXAMPLE.TEST`,
      `${fmt(new Date(NOW))}  ${fmt(late)}  krbtgt/EXAMPLE.TEST@EXAMPLE.TEST`,
      '',
    ].join('\n');
    expect(parseKlistTicket(output, NOW).expiresAt).toBe(late.getTime());
  });
});

describe('perimeter live-status evaluators (issue #154)', () => {
  it('maps CA presence to ok/fail', () => {
    expect(evaluateCaCheck(true)).toEqual({
      id: 'ca',
      state: 'ok',
      reasonCode: 'ca_ok',
    });
    expect(evaluateCaCheck(false)).toEqual({
      id: 'ca',
      state: 'fail',
      reasonCode: 'ca_absent',
    });
  });

  it('maps every Kerberos verdict, and omits the row where kinit is unsupported', () => {
    expect(evaluateKerberosCheck(false, 'none')).toBeNull();
    expect(evaluateKerberosCheck(true, 'valid')?.state).toBe('unknown');
    expect(evaluateKerberosCheck(true, 'expiring_soon')?.state).toBe('degraded');
    expect(evaluateKerberosCheck(true, 'expired')?.state).toBe('fail');
    expect(evaluateKerberosCheck(true, 'none')?.state).toBe('fail');
    expect(evaluateKerberosCheck(true, 'unknown')?.state).toBe('unknown');
  });

  describe('evaluateKerberosCheck acceptance (#283)', () => {
    it('keeps a confirmed valid ticket green and expiring-soon amber', () => {
      expect(evaluateKerberosCheck(true, 'valid', 'carried')).toEqual({
        id: 'kerberos',
        state: 'ok',
        reasonCode: 'ticket_valid',
      });
      expect(evaluateKerberosCheck(true, 'expiring_soon', 'carried')?.reasonCode).toBe(
        'ticket_expiring_soon',
      );
    });

    it('fails a locally-valid ticket the proxy refuses to authorize', () => {
      expect(evaluateKerberosCheck(true, 'valid', 'auth_rejected')).toEqual({
        id: 'kerberos',
        state: 'fail',
        reasonCode: 'ticket_not_accepted',
      });
      expect(evaluateKerberosCheck(true, 'expiring_soon', 'auth_rejected')).toEqual({
        id: 'kerberos',
        state: 'fail',
        reasonCode: 'ticket_not_accepted',
      });
    });

    it('reports honest unknown when acceptance could not be attributed', () => {
      for (const acceptance of ['blocked', 'no_response', 'not_run'] as const) {
        expect(evaluateKerberosCheck(true, 'valid', acceptance)).toEqual({
          id: 'kerberos',
          state: 'unknown',
          reasonCode: 'ticket_valid_unconfirmed',
        });
      }
    });

    it('keeps expiring-soon amber when acceptance is merely unconfirmed', () => {
      expect(evaluateKerberosCheck(true, 'expiring_soon', 'blocked')?.reasonCode).toBe(
        'ticket_expiring_soon',
      );
    });

    it('reports outside-perimeter as honestly unknown, not a false ok (ADR-0022)', () => {
      expect(evaluateKerberosCheck(true, 'valid', 'not_in_play')).toEqual({
        id: 'kerberos',
        state: 'unknown',
        reasonCode: 'ticket_valid_unconfirmed_outside_perimeter',
      });
    });

    it('lets the clearer klist verdicts win over any acceptance', () => {
      expect(evaluateKerberosCheck(true, 'expired', 'auth_rejected')?.reasonCode).toBe(
        'ticket_expired',
      );
      expect(evaluateKerberosCheck(true, 'none', 'carried')?.reasonCode).toBe('ticket_none');
      expect(evaluateKerberosCheck(true, 'unknown', 'auth_rejected')?.reasonCode).toBe(
        'ticket_unknown',
      );
    });
  });

  describe('deriveTicketAcceptance (#283)', () => {
    const routes = (
      over: Partial<Parameters<typeof deriveTicketAcceptance>[0]>,
    ): Parameters<typeof deriveTicketAcceptance>[0] => ({
      goosar: {
        host: 'goosar.ru',
        route: { kind: 'proxy', host: 'isa.corp', port: 8080, scheme: 'http' },
      },
      llm: { host: 'gw.internal', route: { kind: 'direct' } },
      localProxy: {
        host: '127.0.0.1',
        port: 3128,
        listening: true,
        upstreamWorks: true,
        probe: 'carried',
      },
      entry: { kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 },
      conflict: null,
      ...over,
    });

    it('is not in play on a direct route with no proxy running', () => {
      expect(
        deriveTicketAcceptance(
          routes({
            goosar: { host: 'goosar.ru', route: { kind: 'direct' } },
            localProxy: null,
            entry: { kind: 'system', route: { kind: 'direct' } },
          }),
        ),
      ).toBe('not_in_play');
    });

    it('passes the probe verdict through when the perimeter is in play', () => {
      expect(deriveTicketAcceptance(routes({}))).toBe('carried');
      expect(
        deriveTicketAcceptance(
          routes({
            localProxy: {
              host: '127.0.0.1',
              port: 3128,
              listening: true,
              upstreamWorks: false,
              probe: 'auth_rejected',
            },
          }),
        ),
      ).toBe('auth_rejected');
    });

    it('counts a listening proxy as in play even on a direct system route', () => {
      expect(
        deriveTicketAcceptance(
          routes({
            goosar: { host: 'goosar.ru', route: { kind: 'direct' } },
            localProxy: {
              host: '127.0.0.1',
              port: 3128,
              listening: true,
              upstreamWorks: false,
              probe: 'auth_rejected',
            },
          }),
        ),
      ).toBe('auth_rejected');
    });

    it('reports not_run when the perimeter is in play but nothing was probed', () => {
      expect(deriveTicketAcceptance(routes({ localProxy: null }))).toBe('not_run');
    });

    it('falls back to upstreamWorks for facts without a probe field', () => {
      expect(
        deriveTicketAcceptance(
          routes({
            localProxy: {
              host: '127.0.0.1',
              port: 3128,
              listening: true,
              upstreamWorks: true,
            },
          }),
        ),
      ).toBe('carried');
      expect(
        deriveTicketAcceptance(
          routes({
            localProxy: {
              host: '127.0.0.1',
              port: 3128,
              listening: true,
              upstreamWorks: false,
            },
          }),
        ),
      ).toBe('not_run');
    });
  });

  describe('parseKlistCacheIdentity (#283)', () => {
    it("reads the Heimdal cache line and the principal's realm", () => {
      const output = [
        'Credentials cache: API:12345678-ABCD-ABCD-ABCD-123456789ABC',
        '        Principal: alice@EXAMPLE.TEST',
        '',
        '  Issued                Expires               Principal',
        'Jan 10 09:00:00 2026  Jan 10 19:00:00 2026  krbtgt/EXAMPLE.TEST@EXAMPLE.TEST',
      ].join('\n');
      expect(parseKlistCacheIdentity(output)).toEqual({
        cache: 'API:12345678-ABCD-ABCD-ABCD-123456789ABC',
        realm: 'EXAMPLE.TEST',
        principal: 'alice@EXAMPLE.TEST',
      });
    });

    it('returns nulls for output without the identity lines', () => {
      expect(parseKlistCacheIdentity('klist: No ticket file: /tmp/x')).toEqual({
        cache: null,
        realm: null,
        principal: null,
      });
      expect(parseKlistCacheIdentity('')).toEqual({
        cache: null,
        realm: null,
        principal: null,
      });
    });

    it('reads the full principal for the settings panel', () => {
      const identity = parseKlistCacheIdentity(
        'Credentials cache: FILE:/tmp/krb5cc_501\n        Principal: alice@EXAMPLE.COM\n',
      );
      expect(identity.realm).toBe('EXAMPLE.COM');
      expect(identity.principal).toBe('alice@EXAMPLE.COM');
    });
  });

  describe('parseKlistRenewUntil (#466)', () => {
    it("reads Heimdal's `Renew till`", () => {
      const output = [
        'Credentials cache: API:AAA',
        '        Principal: alice@EXAMPLE.TEST',
        '',
        '  Issued                Expires               Principal',
        'Jan 10 09:00:00 2026  Jan 10 19:00:00 2026  krbtgt/EXAMPLE.TEST@EXAMPLE.TEST',
        '\tRenew till: Jan 17 09:00:00 2026',
      ].join('\n');
      expect(parseKlistRenewUntil(output)).toBe(new Date(2026, 0, 17, 9, 0, 0).getTime());
    });

    it("reads MIT's `renew until`", () => {
      expect(parseKlistRenewUntil('\trenew until Mar 3 08:30:00 2026')).toBe(
        new Date(2026, 2, 3, 8, 30, 0).getTime(),
      );
    });

    it('returns null when klist printed no renewable deadline', () => {
      expect(
        parseKlistRenewUntil('Jan 10 09:00:00 2026  Jan 10 19:00:00 2026  krbtgt/A@A'),
      ).toBeNull();
    });
  });

  it('reads any HTTP response < 500 as ok, a 5xx as degraded, and no round-trip as fail', () => {
    const ok = evaluateReachabilityCheck('server', {
      kind: 'response',
      status: 401,
    });
    expect(ok).toEqual({ id: 'server', state: 'ok', reasonCode: 'live_server_ok' });

    const degraded = evaluateReachabilityCheck('server', {
      kind: 'response',
      status: 502,
    });
    expect(degraded.state).toBe('degraded');
    expect(degraded.reasonCode).toBe('live_server_degraded');

    const down = evaluateReachabilityCheck('llm', { kind: 'unreachable' });
    expect(down).toEqual({
      id: 'llm',
      state: 'fail',
      reasonCode: 'live_llm_unreachable',
    });

    for (const fact of [
      { kind: 'unconfigured' } as HttpReachability,
      { kind: 'skipped' } as HttpReachability,
    ]) {
      const unknown = evaluateReachabilityCheck('llm', fact);
      expect(unknown.state).toBe('unknown');
      expect(unknown.reasonCode).toBe('live_llm_unconfigured');
    }
  });

  describe('evaluateLlmAuthCheck (T-23, ADR-0021)', () => {
    it('reads 2xx as ok', () => {
      expect(evaluateLlmAuthCheck({ kind: 'response', status: 200 })).toEqual({
        id: 'llm',
        state: 'ok',
        reasonCode: 'live_llm_ok',
      });
    });

    it('reads 401/403 as a red auth_rejected, never ok', () => {
      for (const status of [401, 403]) {
        expect(evaluateLlmAuthCheck({ kind: 'response', status })).toEqual({
          id: 'llm',
          state: 'fail',
          reasonCode: 'llm_auth_rejected',
        });
      }
    });

    it('reads other error statuses as degraded', () => {
      expect(evaluateLlmAuthCheck({ kind: 'response', status: 500 }).state).toBe('degraded');
      expect(evaluateLlmAuthCheck({ kind: 'response', status: 404 }).state).toBe('degraded');
    });

    it('reads unreachable as fail', () => {
      expect(evaluateLlmAuthCheck({ kind: 'unreachable' })).toEqual({
        id: 'llm',
        state: 'fail',
        reasonCode: 'live_llm_unreachable',
      });
    });

    it('reads no_key and unconfigured as unknown, never a guessed pass/fail', () => {
      expect(evaluateLlmAuthCheck({ kind: 'no_key' })).toEqual({
        id: 'llm',
        state: 'unknown',
        reasonCode: 'llm_key_not_configured',
      });
      expect(evaluateLlmAuthCheck({ kind: 'unconfigured' }).state).toBe('unknown');
      expect(evaluateLlmAuthCheck({ kind: 'skipped' }).state).toBe('unknown');
    });
  });

  describe('evaluateKerberosCheck principal mismatch (T-31, ADR-0022)', () => {
    it('fails when the cached principal differs from the resolved one', () => {
      expect(
        evaluateKerberosCheck(true, 'valid', 'carried', {
          cachePrincipal: 'alice@CORP.EXAMPLE',
          resolvedPrincipal: 'alice.smith@CORP.EXAMPLE',
        }),
      ).toEqual({ id: 'kerberos', state: 'fail', reasonCode: 'principal_mismatch' });
    });

    it('wins over a carried acceptance', () => {
      const check = evaluateKerberosCheck(true, 'expiring_soon', 'carried', {
        cachePrincipal: 'bob@CORP.EXAMPLE',
        resolvedPrincipal: 'robert@CORP.EXAMPLE',
      });
      expect(check?.reasonCode).toBe('principal_mismatch');
    });

    it('does not fire when principals match or are unknown', () => {
      expect(
        evaluateKerberosCheck(true, 'valid', 'carried', {
          cachePrincipal: 'alice@CORP.EXAMPLE',
          resolvedPrincipal: 'alice@CORP.EXAMPLE',
        })?.reasonCode,
      ).toBe('ticket_valid');
      expect(
        evaluateKerberosCheck(true, 'valid', 'carried', {
          cachePrincipal: null,
          resolvedPrincipal: 'alice@CORP.EXAMPLE',
        })?.reasonCode,
      ).toBe('ticket_valid');
    });
  });

  describe('deriveTicketSource (T-31)', () => {
    it('reads a successful in-app kinit as kinit_app', () => {
      expect(deriveTicketSource({ at: 1, kind: 'kinit', ok: true })).toBe('kinit_app');
    });

    it('reads a successful kinit -R as renewed', () => {
      expect(deriveTicketSource({ at: 1, kind: 'renew', ok: true })).toBe('renewed');
    });

    it('falls back to cache on a failed attempt or no attempt this session', () => {
      expect(deriveTicketSource({ at: 1, kind: 'kinit', ok: false, reason: 'x' })).toBe('cache');
      expect(deriveTicketSource(null)).toBe('cache');
      expect(deriveTicketSource(undefined)).toBe('cache');
    });
  });

  it('maps daemon health to running/stopped/unknown', () => {
    expect(evaluateDaemonCheck({ kind: 'running' })).toEqual({
      id: 'daemon',
      state: 'ok',
      reasonCode: 'live_daemon_ok',
    });
    expect(evaluateDaemonCheck({ kind: 'stopped' })).toEqual({
      id: 'daemon',
      state: 'fail',
      reasonCode: 'live_daemon_stopped',
    });
    for (const fact of [
      { kind: 'unknown' } as DaemonReachability,
      { kind: 'skipped' } as DaemonReachability,
    ]) {
      expect(evaluateDaemonCheck(fact).state).toBe('unknown');
    }
  });

  it('maps provisioning sync status to ok/syncing/fail with package counts (issue #188)', () => {
    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'ok',
        installed: 12,
        total: 12,
        failed: 0,
      }),
    ).toEqual({
      id: 'provisioning',
      state: 'ok',
      reasonCode: 'live_provisioning_ok',
      counts: { installed: 12, total: 12 },
    });

    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'syncing',
        installed: 5,
        total: 12,
        failed: 0,
      }),
    ).toEqual({
      id: 'provisioning',
      state: 'degraded',
      reasonCode: 'live_provisioning_pending',
      counts: { installed: 5, total: 12 },
    });

    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'fail',
        installed: 10,
        total: 12,
        failed: 2,
      }),
    ).toEqual({
      id: 'provisioning',
      state: 'fail',
      reasonCode: 'live_provisioning_fail',
      counts: { installed: 10, total: 12 },
    });

    for (const fact of [
      { kind: 'unknown' } as ProvisioningReachability,
      { kind: 'skipped' } as ProvisioningReachability,
    ]) {
      const result = evaluateProvisioningCheck(fact);
      expect(result.state).toBe('unknown');
      expect(result.reasonCode).toBe('live_provisioning_unknown');
      expect(result.counts).toBeUndefined();
    }
  });

  it('reports a deferred daemon restart as its own degraded state, never a silent ok — issue #191', () => {
    const result = evaluateProvisioningCheck({
      kind: 'status',
      state: 'ok',
      installed: 12,
      total: 12,
      failed: 0,
      restartPending: true,
    });
    expect(result).toEqual({
      id: 'provisioning',
      state: 'degraded',
      reasonCode: 'live_provisioning_restart_pending',
      counts: { installed: 12, total: 12 },
      restartPending: true,
    });
    expect(result.reasonCode).not.toBe('live_provisioning_ok');
    expect(result.reasonCode).not.toBe('live_provisioning_fail');
  });

  it('passes preserved pre-0.7.0 legacy paths through onto the check regardless of sync state — issue #191', () => {
    const paths = ['/Users/x/.hermes/skills/foo.pre-0.7.0'];
    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'ok',
        installed: 12,
        total: 12,
        failed: 0,
        preservedLegacyPaths: paths,
      }).preservedLegacyPaths,
    ).toEqual(paths);

    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'fail',
        installed: 10,
        total: 12,
        failed: 2,
        preservedLegacyPaths: paths,
      }).preservedLegacyPaths,
    ).toEqual(paths);

    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'ok',
        installed: 12,
        total: 12,
        failed: 0,
      }).preservedLegacyPaths,
    ).toBeUndefined();
  });

  it('surfaces server-revoked package removals as labels on the check — issue #242, T-060 (the panel must show the removal, never stay silent)', () => {
    const removed = [
      { name: 'outlook', type: 'mcp-server', version: '1.2.0', state: 'removed' as const },
      { name: 'office-docx', type: 'skill', state: 'pending' as const },
    ];
    const check = evaluateProvisioningCheck({
      kind: 'status',
      state: 'ok',
      installed: 3,
      total: 3,
      failed: 0,
      removedPackages: removed,
    });
    expect(check.removedPackages).toEqual(['mcp-server:outlook', 'skill:office-docx…']);

    expect(
      evaluateProvisioningCheck({
        kind: 'status',
        state: 'ok',
        installed: 3,
        total: 3,
        failed: 0,
      }).removedPackages,
    ).toBeUndefined();
  });

  it('keeps removal labels visible in the UNCONFIGURED branch — issue #191 pass-2 M (disabling the only pin removes the package AND the removal row must survive)', () => {
    const check = evaluateProvisioningCheck({
      kind: 'unconfigured',
      removedPackages: [
        { name: 'office-docx', type: 'skill', version: '1.0.0', state: 'removed' as const },
        { name: 'outlook', type: 'mcp-server', state: 'pending' as const },
      ],
    });
    expect(check.state).toBe('ok');
    expect(check.reasonCode).toBe('live_provisioning_not_configured');
    expect(check.removedPackages).toEqual(['skill:office-docx', 'mcp-server:outlook…']);

    expect(evaluateProvisioningCheck({ kind: 'unconfigured' }).removedPackages).toBeUndefined();
  });

  it('reports "not configured" as ok, never as failed or unknown — issue #188 (a deployment that serves no packages is a normal state)', () => {
    const result = evaluateProvisioningCheck({ kind: 'unconfigured' });
    expect(result).toEqual({
      id: 'provisioning',
      state: 'ok',
      reasonCode: 'live_provisioning_not_configured',
    });
    expect(result.reasonCode).not.toBe('live_provisioning_fail');
    expect(result.reasonCode).not.toBe('live_provisioning_pending');
  });

  it('rolls up worst-wins, with unknown floored to amber and empty to unknown', () => {
    const c = (state: PerimeterCheck['state']): PerimeterCheck => ({
      id: 'ca',
      state,
      reasonCode: 'x',
    });
    expect(aggregateLiveOverall([])).toBe('unknown');
    expect(aggregateLiveOverall([c('ok'), c('ok')])).toBe('ok');
    expect(aggregateLiveOverall([c('ok'), c('degraded'), c('fail')])).toBe('fail');
    expect(aggregateLiveOverall([c('ok'), c('degraded')])).toBe('degraded');
    expect(aggregateLiveOverall([c('ok'), c('unknown')])).toBe('degraded');
  });
});

describe('buildPerimeterLiveStatus (issue #154)', () => {
  const HEALTHY: PerimeterLiveInputs = {
    caBundlePresent: true,
    routes: {
      goosar: {
        host: 'goosar.ru',
        route: { kind: 'proxy', host: 'isa.corp', port: 8080, scheme: 'http' },
      },
      llm: { host: 'gw.internal', route: { kind: 'direct' } },
      localProxy: {
        host: '127.0.0.1',
        port: 3128,
        listening: true,
        upstreamWorks: true,
      },
      entry: { kind: 'loopback_proxy', host: '127.0.0.1', port: 3128 },
      conflict: null,
    },
    kerberosSupported: true,
    kerberosTicket: 'valid',
    server: { kind: 'response', status: 200 },
    llm: { kind: 'response', status: 200 },
    daemon: { kind: 'running' },
    provisioning: { kind: 'status', state: 'ok', installed: 3, total: 3, failed: 0 },
    checkedAt: 1_000,
  };

  it('is all-green and overall ok when every subsystem really works', () => {
    const status = buildPerimeterLiveStatus(HEALTHY);
    expect(status.overall).toBe('ok');
    expect(status.checkedAt).toBe(1_000);
    expect(status.checks.map((check) => check.id)).toEqual([
      'route_server',
      'route_llm',
      'entry',
      'px',
      'ca',
      'kerberos',
      'server',
      'llm',
      'daemon',
      'provisioning',
    ]);
    expect(status.checks.every((check) => check.state === 'ok')).toBe(true);
  });

  it('fails the kerberos row when the proxy rejects a locally-valid ticket (#283)', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      routes: {
        ...HEALTHY.routes,
        localProxy: {
          host: '127.0.0.1',
          port: 3128,
          listening: true,
          upstreamWorks: false,
          probe: 'auth_rejected',
        },
        entry: {
          kind: 'system',
          route: { kind: 'proxy', host: 'isa.corp', port: 8080, scheme: 'http' },
        },
      },
    });
    expect(status.overall).toBe('fail');
    const kerberos = status.checks.find((check) => check.id === 'kerberos');
    expect(kerberos).toEqual({
      id: 'kerberos',
      state: 'fail',
      reasonCode: 'ticket_not_accepted',
    });
  });

  it('reads an unattributable probe failure as unconfirmed, never as green (#283)', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      routes: {
        ...HEALTHY.routes,
        localProxy: {
          host: '127.0.0.1',
          port: 3128,
          listening: true,
          upstreamWorks: false,
          probe: 'blocked',
        },
        entry: {
          kind: 'system',
          route: { kind: 'proxy', host: 'isa.corp', port: 8080, scheme: 'http' },
        },
      },
    });
    const kerberos = status.checks.find((check) => check.id === 'kerberos');
    expect(kerberos?.state).toBe('unknown');
    expect(kerberos?.reasonCode).toBe('ticket_valid_unconfirmed');
  });

  it('surfaces an incomplete package sync as degraded, not a hidden gap', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      provisioning: { kind: 'status', state: 'syncing', installed: 1, total: 3, failed: 0 },
    });
    expect(status.overall).toBe('degraded');
    const provisioning = status.checks.find((check) => check.id === 'provisioning');
    expect(provisioning).toEqual({
      id: 'provisioning',
      state: 'degraded',
      reasonCode: 'live_provisioning_pending',
      counts: { installed: 1, total: 3 },
    });
  });

  it('surfaces the failing subsystem with its reason and a red overall — the daemon-down case the toggle used to hide', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      daemon: { kind: 'stopped' },
    });
    expect(status.overall).toBe('fail');
    const daemon = status.checks.find((check) => check.id === 'daemon');
    expect(daemon).toEqual({
      id: 'daemon',
      state: 'fail',
      reasonCode: 'live_daemon_stopped',
    });
  });

  it('reports a 502-via-px LLM gateway as degraded (reachable but erroring), not green or down', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      llm: { kind: 'response', status: 502 },
    });
    expect(status.overall).toBe('degraded');
    const llm = status.checks.find((check) => check.id === 'llm');
    expect(llm?.state).toBe('degraded');
    expect(llm?.reasonCode).toBe('live_llm_degraded');
  });

  it('drops the Kerberos row on platforms without in-app kinit', () => {
    const status = buildPerimeterLiveStatus({
      ...HEALTHY,
      kerberosSupported: false,
    });
    expect(status.checks.some((check) => check.id === 'kerberos')).toBe(false);
    expect(status.overall).toBe('ok');
  });
});

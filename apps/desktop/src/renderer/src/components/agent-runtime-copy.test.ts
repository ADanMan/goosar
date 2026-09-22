import { describe, it, expect } from 'vitest';
import enSettings from '@goosar/views/locales/en/settings.json';
import ruSettings from '@goosar/views/locales/ru/settings.json';
import {
  agentConfigFieldLabel,
  agentRuntimeDescription,
  agentRuntimeLabel,
  daemonStateLabel,
  listFieldLabels,
  mcpClientSentence,
} from './agent-runtime-copy';
import type { AgentRuntimeStatus } from '../../../shared/agent-runtime-types';

function fakeT(bundle: unknown) {
  return ((selector: (b: never) => string, vars?: Record<string, unknown>) => {
    const raw = selector(bundle as never);
    if (typeof raw !== 'string') throw new Error('missing key');
    return vars
      ? raw.replace(/\{\{(\w+)\}\}/g, (_m, name: string) => String(vars[name] ?? ''))
      : raw;
  }) as never;
}

const en = fakeT(enSettings);
const ru = fakeT(ruSettings);

describe('agent runtime copy', () => {
  it('has a Russian sentence for every runtime state', () => {
    const states: AgentRuntimeStatus[] = [
      { state: 'checking' },
      { state: 'unsupported', detail: 'Windows is not supported yet.' },
      { state: 'not_installed', detail: 'Nothing to run.' },
      { state: 'external', path: '/usr/local/bin/hermes' },
      {
        state: 'needs_config',
        version: '0.14.0',
        binPath: '/b',
        configPath: '/c/config.user.yaml',
        missing: ['llm.api_base', 'llm.model'],
      },
      {
        state: 'ready',
        version: '0.14.0',
        binPath: '/b',
        configPath: '/c/config.user.yaml',
      },
    ];
    for (const status of states) {
      const text = agentRuntimeDescription(ru, status);
      expect(text.length, status.state).toBeGreaterThan(0);
      expect(agentRuntimeLabel(ru, status.state), status.state).toBeTruthy();
      if (status.state !== 'unsupported') {
        expect(/[а-яА-Я]/.test(text), status.state).toBe(true);
      }
    }
  });

  it('interpolates the identifiers instead of translating them', () => {
    const status: AgentRuntimeStatus = {
      state: 'needs_config',
      version: '0.14.0',
      binPath: '/b',
      configPath: '/home/me/.hermes/config.user.yaml',
      missing: ['llm.api_key'],
    };
    const text = agentRuntimeDescription(ru, status);
    expect(text).toContain('0.14.0');
    expect(text).toContain('/home/me/.hermes/config.user.yaml');
  });

  it('says nothing extra when the MCP contract holds', () => {
    expect(mcpClientSentence(en, { state: 'ok', version: '1.9.0' })).toBe('');
    expect(mcpClientSentence(en, undefined)).toBe('');
  });

  it('carries the MCP verdict, with its reason, onto the runtime sentence', () => {
    const status: AgentRuntimeStatus = {
      state: 'ready',
      version: '0.14.0',
      binPath: '/b',
      configPath: '/c',
      mcpClient: { state: 'incompatible', version: '2.0.0', reason: 'tool fields renamed' },
    };
    expect(agentRuntimeDescription(en, status)).toMatch(/MCP integrations are unavailable/i);
    expect(agentRuntimeDescription(en, status)).toContain('tool fields renamed');
    expect(agentRuntimeDescription(ru, status)).toContain('tool fields renamed');
  });

  it('warns about a second hermes the app does not manage', () => {
    const status: AgentRuntimeStatus = {
      state: 'ready',
      version: '0.14.0',
      binPath: '/managed/hermes',
      configPath: '/c',
      userBinConflict: '/usr/local/bin/hermes',
    };
    expect(agentRuntimeDescription(ru, status)).toContain('/usr/local/bin/hermes');
  });

  it("joins field labels with the bundle's own conjunction", () => {
    const joined = listFieldLabels(ru, ['llm.api_base', 'llm.model']);
    expect(joined).toBe(
      `${agentConfigFieldLabel(ru, 'llm.api_base')} и ${agentConfigFieldLabel(ru, 'llm.model')}`,
    );
    expect(listFieldLabels(ru, [])).toBe('');
  });

  it('has a Russian word for every daemon state', () => {
    for (const state of [
      'running',
      'stopped',
      'starting',
      'stopping',
      'installing_cli',
      'cli_not_found',
      'auth_expired',
    ] as const) {
      const label = daemonStateLabel(ru, state);
      expect(/[а-яА-Я]/.test(label), state).toBe(true);
    }
  });
});

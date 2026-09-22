import { describe, expect, it } from 'vitest';
import {
  parseRuntimeFailure,
  runtimeFailureCause,
  usesRuntimeMessage,
} from './runtime-failure-signature';

const AGENT_MISSING =
  "hermes session/new failed: session/new: Internal error (code=-32603, data=agent 'hermes' not found and no usable default_agent)";

const LLM_NOT_CONFIGURED =
  'hermes session/new failed: session/new: /Users/e/.hermes/config.user.yaml: llm.api_key is still the placeholder (code=-32603, data=llm_not_configured)';

describe('parseRuntimeFailure', () => {
  it('recovers the code and data the daemon flattened into text', () => {
    const signature = parseRuntimeFailure(AGENT_MISSING);

    expect(signature?.code).toBe(-32603);
    expect(signature?.data).toBe("agent 'hermes' not found and no usable default_agent");
  });

  it('drops a bare JSON-RPC label, which carries no information', () => {
    expect(parseRuntimeFailure(AGENT_MISSING)?.message).toBe('');
  });

  it("keeps the runtime's own message once the wrappers are peeled", () => {
    expect(parseRuntimeFailure(LLM_NOT_CONFIGURED)?.message).toBe(
      '/Users/e/.hermes/config.user.yaml: llm.api_key is still the placeholder',
    );
  });

  it('handles a frame with no data field', () => {
    const signature = parseRuntimeFailure(
      'kimi session/new failed: session/new: Session not found (code=-32602)',
    );

    expect(signature).toEqual({
      code: -32602,
      data: null,
      message: 'Session not found',
    });
  });

  it('returns null for text that is not a JSON-RPC frame', () => {
    expect(parseRuntimeFailure('connection reset by peer')).toBeNull();
    expect(parseRuntimeFailure('')).toBeNull();
    expect(parseRuntimeFailure(null)).toBeNull();
    expect(parseRuntimeFailure(undefined)).toBeNull();
  });

  it('does not eat a colon that belongs to the message itself', () => {
    expect(parseRuntimeFailure('/etc/hermes/config.yaml: unreadable (code=-32603)')?.message).toBe(
      '/etc/hermes/config.yaml: unreadable',
    );
  });
});

describe('runtimeFailureCause', () => {
  it("names the unconfigured-model case from the runtime's own identifier", () => {
    expect(runtimeFailureCause(parseRuntimeFailure(LLM_NOT_CONFIGURED))).toBe('llm_not_configured');
  });

  it('names the unresolved-agent case from the free-text data field', () => {
    expect(runtimeFailureCause(parseRuntimeFailure(AGENT_MISSING))).toBe('agent_unresolved');
  });

  it('returns null for causes it has nothing better to say about', () => {
    expect(
      runtimeFailureCause(parseRuntimeFailure('hermes prompt failed: prompt: boom (code=-32000)')),
    ).toBeNull();
    expect(runtimeFailureCause(null)).toBeNull();
  });
});

describe('usesRuntimeMessage', () => {
  it('defers to the runtime for llm_not_configured', () => {
    const signature = parseRuntimeFailure(LLM_NOT_CONFIGURED);
    expect(usesRuntimeMessage(runtimeFailureCause(signature), signature)).toBe(true);
  });

  it('does not defer when the runtime only sent the generic label', () => {
    const signature = parseRuntimeFailure(
      'hermes session/new failed: session/new: Internal error (code=-32603, data=llm_not_configured)',
    );
    expect(usesRuntimeMessage(runtimeFailureCause(signature), signature)).toBe(false);
  });

  it('does not defer for causes the product has its own copy for', () => {
    const signature = parseRuntimeFailure(AGENT_MISSING);
    expect(usesRuntimeMessage(runtimeFailureCause(signature), signature)).toBe(false);
  });
});

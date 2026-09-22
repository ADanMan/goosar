import type { Decision, EvalContext, Provider } from './types';

export class FeatureFlagService {
  private provider: Provider | null;

  constructor(provider: Provider | null = null) {
    this.provider = provider;
  }

  setProvider(provider: Provider | null): void {
    this.provider = provider;
  }

  isEnabled(key: string, ctx: EvalContext, defaultValue: boolean): boolean {
    return this.decision(key, ctx, defaultValue).enabled;
  }

  variant(key: string, ctx: EvalContext, defaultValue: string): string {
    if (!this.provider) {
      return defaultValue;
    }
    const d = this.provider.lookup(key, ctx);
    if (!d) return defaultValue;
    return d.variant;
  }

  decision(key: string, ctx: EvalContext, defaultValue: boolean): Decision {
    if (!this.provider) {
      return defaultDecision(key, defaultValue);
    }
    const d = this.provider.lookup(key, ctx);
    if (!d) return defaultDecision(key, defaultValue);
    return { ...d, key };
  }

  getProvider(): Provider | null {
    return this.provider;
  }
}

function defaultDecision(key: string, value: boolean): Decision {
  return {
    key,
    enabled: value,
    variant: value ? 'on' : 'off',
    reason: 'default',
    source: 'default',
  };
}

import type { Decision, EvalContext, Provider } from './types';

export class ChainProvider implements Provider {
  readonly name = 'chain';
  private readonly providers: ReadonlyArray<Provider>;

  constructor(providers: ReadonlyArray<Provider | null | undefined>) {
    this.providers = providers.filter((p): p is Provider => p != null);
  }

  lookup(key: string, ctx: EvalContext): Decision | undefined {
    for (const p of this.providers) {
      const d = p.lookup(key, ctx);
      if (d !== undefined) return d;
    }
    return undefined;
  }
}

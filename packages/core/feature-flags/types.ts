// Публичные типы feature-flags; форма зеркалит server/pkg/featureflag,
// чтобы Decision от backend читался без преобразований.

export type Reason = 'static' | 'percent' | 'override' | 'default' | 'error';

export interface Decision {
  key: string;
  enabled: boolean;
  variant: string;
  reason: Reason;
  source: string;
}

export interface EvalContext {
  userId?: string;
  workspaceId?: string;
  attributes?: Readonly<Record<string, string>>;
}

export interface PercentRollout {
  percent: number;
  by?: string;
}

export interface Rule {
  default?: boolean;
  variant?: string;
  allow?: ReadonlyArray<string>;
  allowBy?: string;
  deny?: ReadonlyArray<string>;
  denyBy?: string;
  percent?: PercentRollout;
}

export interface Provider {
  readonly name: string;
  lookup(key: string, ctx: EvalContext): Decision | undefined;
}

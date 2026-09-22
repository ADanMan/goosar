import type { Decision, EvalContext, Provider, Rule } from './types';
import { inPercent } from './hash';

export class StaticProvider implements Provider {
  readonly name = 'static';
  private rules: Map<string, Rule>;

  constructor(rules: Readonly<Record<string, Rule>> = {}) {
    this.rules = new Map(Object.entries(rules));
  }

  set(key: string, rule: Rule): void {
    this.rules.set(key, rule);
  }

  loadRules(rules: Readonly<Record<string, Rule>>): void {
    this.rules = new Map(Object.entries(rules));
  }

  keys(): string[] {
    return Array.from(this.rules.keys()).sort();
  }

  lookup(key: string, ctx: EvalContext): Decision | undefined {
    const rule = this.rules.get(key);
    if (!rule) return undefined;
    return evaluateRule(key, rule, ctx);
  }
}

function evaluateRule(key: string, rule: Rule, ctx: EvalContext): Decision {
  const denyBy = rule.denyBy ?? 'user_id';
  if (rule.deny && rule.deny.length > 0) {
    const v = lookupAttr(ctx, denyBy);
    if (v && rule.deny.includes(v)) {
      return decisionFromRule(key, rule, false, 'static');
    }
  }

  const allowBy = rule.allowBy ?? 'user_id';
  if (rule.allow && rule.allow.length > 0) {
    const v = lookupAttr(ctx, allowBy);
    if (v && rule.allow.includes(v)) {
      return decisionFromRule(key, rule, true, 'static');
    }
  }

  if (rule.percent) {
    const by = rule.percent.by ?? 'user_id';
    const ident = lookupAttr(ctx, by) ?? '';
    const enabled = inPercent(key, ident, rule.percent.percent);
    return decisionFromRule(key, rule, enabled, 'percent');
  }

  return decisionFromRule(key, rule, rule.default ?? false, 'static');
}

function decisionFromRule(
  key: string,
  rule: Rule,
  enabled: boolean,
  reason: Decision['reason'],
): Decision {
  let variant = boolToVariant(enabled);
  if (enabled && rule.variant && rule.variant.length > 0) {
    variant = rule.variant;
  }
  return {
    key,
    enabled,
    variant,
    reason,
    source: 'static',
  };
}

function boolToVariant(b: boolean): string {
  return b ? 'on' : 'off';
}

function lookupAttr(ctx: EvalContext, name: string): string | undefined {
  if (name === 'user_id') return nonEmpty(ctx.userId);
  if (name === 'workspace_id') return nonEmpty(ctx.workspaceId);
  return nonEmpty(ctx.attributes?.[name]);
}

function nonEmpty(v: string | undefined): string | undefined {
  return v && v.length > 0 ? v : undefined;
}

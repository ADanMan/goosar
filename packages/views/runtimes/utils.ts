import type { AgentRuntime, RuntimeUsage, RuntimeUsageByAgent } from '@goosar/core/types';
import { getCustomPricing } from '@goosar/core/runtimes/custom-pricing-store';

export function isSelfHealingRuntime(runtime: AgentRuntime): boolean {
  return runtime.runtime_mode === 'local' && runtime.status === 'online';
}

export function formatLastSeen(lastSeenAt: string | null): string {
  if (!lastSeenAt) return 'Never';
  const diffMs = Date.now() - new Date(lastSeenAt).getTime();
  if (diffMs < 5_000) return 'Just now';

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (minutes < 1) return `${seconds}s ago`;
  if (hours < 1) {
    const s = seconds % 60;
    return s > 0 ? `${minutes}m ${s}s ago` : `${minutes}m ago`;
  }
  if (days < 1) {
    const m = minutes % 60;
    return m > 0 ? `${hours}h ${m}m ago` : `${hours}h ago`;
  }
  const h = hours % 24;
  return h > 0 ? `${days}d ${h}h ago` : `${days}d ago`;
}

export function formatDeviceInfo(raw: string | null): string | null {
  if (!raw) return null;
  const trimmed = raw.trim();
  if (!trimmed) return null;
  return trimmed
    .split(' · ')
    .map((part) => prettifyOsArch(part))
    .join(' · ');
}

function prettifyOsArch(part: string): string {
  const lower = part.toLowerCase();
  const match = lower.match(
    /^(darwin|linux|windows|freebsd|openbsd|netbsd)-(amd64|arm64|386|arm)$/,
  );
  if (!match) return part;
  const os = match[1] ?? '';
  const arch = match[2] ?? '';
  const osLabel = OS_LABEL[os] ?? os;
  const archLabel = ARCH_LABEL[arch] ?? arch;
  return `${osLabel} (${archLabel})`;
}

const OS_LABEL: Record<string, string> = {
  darwin: 'macOS',
  linux: 'Linux',
  windows: 'Windows',
  freebsd: 'FreeBSD',
  openbsd: 'OpenBSD',
  netbsd: 'NetBSD',
};

const ARCH_LABEL: Record<string, string> = {
  amd64: 'x86_64',
  arm64: 'arm64',
  '386': 'x86',
  arm: 'arm',
};

function stripVersionPrefix(v: string): string {
  return v.replace(/^v/, '');
}

export function isVersionNewer(latest: string, current: string): boolean {
  const l = stripVersionPrefix(latest).split('.').map(Number);
  const c = stripVersionPrefix(current).split('.').map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0;
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return m % 1 < 0.05 ? `${Math.round(m)}M` : `${m.toFixed(1)}M`;
  }
  if (n >= 1_000) {
    const k = n / 1_000;
    return k % 1 < 0.05 ? `${Math.round(k)}K` : `${k.toFixed(1)}K`;
  }
  return String(n);
}

const MODEL_PRICING: Record<
  string,
  { input: number; output: number; cacheRead: number; cacheWrite: number }
> = {
  'claude-sonnet-5': { input: 2, output: 10, cacheRead: 0.2, cacheWrite: 2.5 },
  'claude-fable-5': { input: 10, output: 50, cacheRead: 1.0, cacheWrite: 12.5 },
  'claude-opus-5': { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  'claude-haiku-4-5': { input: 1, output: 5, cacheRead: 0.1, cacheWrite: 1.25 },
  'claude-sonnet-4-5': { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  'claude-sonnet-4-6': { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  'claude-opus-4-5': { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  'claude-opus-4-6': { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  'claude-opus-4-7': { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  'claude-opus-4-8': { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },

  'claude-opus-4-1': { input: 15, output: 75, cacheRead: 1.5, cacheWrite: 18.75 },
  'claude-opus-4': { input: 15, output: 75, cacheRead: 1.5, cacheWrite: 18.75 },

  'claude-sonnet-4': { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },

  'claude-haiku-3-5': { input: 0.8, output: 4, cacheRead: 0.08, cacheWrite: 1.0 },

  'gpt-5.6-sol': { input: 5, output: 30, cacheRead: 0.5, cacheWrite: 6.25 },
  'gpt-5.6-terra': { input: 2.5, output: 15, cacheRead: 0.25, cacheWrite: 3.125 },
  'gpt-5.6-luna': { input: 1, output: 6, cacheRead: 0.1, cacheWrite: 1.25 },
  'gpt-5.5': { input: 5, output: 30, cacheRead: 0.5, cacheWrite: 5 },
  'gpt-5.4-mini': { input: 0.75, output: 4.5, cacheRead: 0.075, cacheWrite: 0.75 },
  'gpt-5.4': { input: 2.5, output: 15, cacheRead: 0.25, cacheWrite: 2.5 },
  'gpt-5.3-codex': { input: 1.75, output: 14, cacheRead: 0.175, cacheWrite: 1.75 },

  'gpt-5-codex': { input: 1.25, output: 10, cacheRead: 0.125, cacheWrite: 1.25 },
  'gpt-5-mini': { input: 0.25, output: 2, cacheRead: 0.025, cacheWrite: 0.25 },
  'gpt-5-nano': { input: 0.05, output: 0.4, cacheRead: 0.005, cacheWrite: 0.05 },
  'gpt-5': { input: 1.25, output: 10, cacheRead: 0.125, cacheWrite: 1.25 },

  'o3-mini': { input: 1.1, output: 4.4, cacheRead: 0.55, cacheWrite: 1.1 },
  o3: { input: 2, output: 8, cacheRead: 0.5, cacheWrite: 2 },
  'o4-mini': { input: 1.1, output: 4.4, cacheRead: 0.275, cacheWrite: 1.1 },

  'gpt-4o-mini': { input: 0.15, output: 0.6, cacheRead: 0.075, cacheWrite: 0.15 },
  'gpt-4o': { input: 2.5, output: 10, cacheRead: 1.25, cacheWrite: 2.5 },

  'deepseek-v4-flash': { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  'deepseek-v4-pro': { input: 1.74, output: 3.48, cacheRead: 0.0145, cacheWrite: 1.74 },
  'deepseek-chat': { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },
  'deepseek-reasoner': { input: 0.14, output: 0.28, cacheRead: 0.0028, cacheWrite: 0.14 },

  'kimi-k2.6': { input: 0.95, output: 4.0, cacheRead: 0.16, cacheWrite: 0.95 },

  'glm-5.1': { input: 1.4, output: 4.4, cacheRead: 0.26, cacheWrite: 1.4 },
  'glm-5': { input: 1.0, output: 3.2, cacheRead: 0.2, cacheWrite: 1.0 },
  'glm-5-turbo': { input: 1.2, output: 4.0, cacheRead: 0.24, cacheWrite: 1.2 },
  'glm-4.7': { input: 0.6, output: 2.2, cacheRead: 0.11, cacheWrite: 0.6 },
  'glm-4.7-flashx': { input: 0.07, output: 0.4, cacheRead: 0.01, cacheWrite: 0.07 },
  'glm-4.7-flash': { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
  'glm-4.6': { input: 0.6, output: 2.2, cacheRead: 0.11, cacheWrite: 0.6 },
  'glm-4.5': { input: 0.6, output: 2.2, cacheRead: 0.11, cacheWrite: 0.6 },
  'glm-4.5-x': { input: 2.2, output: 8.9, cacheRead: 0.45, cacheWrite: 2.2 },
  'glm-4.5-air': { input: 0.2, output: 1.1, cacheRead: 0.03, cacheWrite: 0.2 },
  'glm-4.5-airx': { input: 1.1, output: 4.5, cacheRead: 0.22, cacheWrite: 1.1 },
  'glm-4.5-flash': { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },

  'grok-4.5': { input: 2, output: 6, cacheRead: 0.3, cacheWrite: 2 },
  'grok-4.3': { input: 1.25, output: 2.5, cacheRead: 0.2, cacheWrite: 1.25 },
  'grok-build-0.1': { input: 1, output: 2, cacheRead: 0.2, cacheWrite: 1 },
  'grok-4.20-multi-agent-0309': { input: 1.25, output: 2.5, cacheRead: 0.2, cacheWrite: 1.25 },
  'grok-4.20-0309-reasoning': { input: 1.25, output: 2.5, cacheRead: 0.2, cacheWrite: 1.25 },
  'grok-4.20-0309-non-reasoning': { input: 1.25, output: 2.5, cacheRead: 0.2, cacheWrite: 1.25 },

  'runtime-g/auto': { input: 1.25, output: 6, cacheRead: 0.25, cacheWrite: 0 },
  'runtime-g/composer-2.5-fast': { input: 3, output: 15, cacheRead: 0.5, cacheWrite: 0 },
  'runtime-g/composer-2.5': { input: 0.5, output: 2.5, cacheRead: 0.2, cacheWrite: 0 },
  'runtime-g/composer-2-fast': { input: 1.5, output: 7.5, cacheRead: 0.35, cacheWrite: 0 },
  'runtime-g/composer-2': { input: 0.5, output: 2.5, cacheRead: 0.2, cacheWrite: 0 },
  'runtime-g/composer-1.5': { input: 3.5, output: 17.5, cacheRead: 0.35, cacheWrite: 0 },
  'runtime-g/composer-1': { input: 1.25, output: 10, cacheRead: 0.125, cacheWrite: 0 },
  cursor: { input: 3, output: 15, cacheRead: 0.5, cacheWrite: 0 },
};

function resolvePricing(model: string, provider?: string) {
  if (!model) return undefined;

  const candidates = pricingCandidates(model, provider);
  for (const candidate of candidates) {
    const hit = MODEL_PRICING[candidate];
    if (hit) return hit;
  }
  for (const candidate of candidates) {
    const hit = getCustomPricing(candidate);
    if (hit) return hit;
  }
  return undefined;
}

function normalizeProvider(provider?: string): string {
  return provider?.trim().toLowerCase() ?? '';
}

function qualify(provider: string, key: string): string {
  return key.startsWith(`${provider}/`) ? key : `${provider}/${key}`;
}

function pricingCandidates(model: string, provider?: string): string[] {
  const base = canonicalCandidates(model);
  const p = normalizeProvider(provider);
  if (!p) return base;
  return [...base.map((c) => qualify(p, c)), ...base];
}

export function pricingKey(model: string, provider?: string): string {
  const p = normalizeProvider(provider);
  return p ? qualify(p, model) : model;
}

export function modelGroupingKey(model: string, provider?: string): string {
  if (!model) return normalizeProvider(provider) || 'unknown';
  return isSelfResolvingId(model) ? model : pricingKey(model, provider);
}

function isSelfResolvingId(model: string): boolean {
  return isModelPriced(model);
}

const canonicalCandidatesCache = new Map<string, string[]>();
function canonicalCandidates(model: string): string[] {
  const cached = canonicalCandidatesCache.get(model);
  if (cached) return cached;
  const seen = new Set<string>();
  const out: string[] = [];
  const push = (s: string) => {
    if (!s || seen.has(s)) return;
    seen.add(s);
    out.push(s);
  };
  const stripDate = (s: string) => s.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, '');
  const stripProvider = (s: string) => {
    const i = s.indexOf('/');
    return i > 0 && /^[a-z][a-z0-9_-]*$/i.test(s.slice(0, i)) ? s.slice(i + 1) : s;
  };
  const canonAnthropic = (s: string) => (s.startsWith('claude-') ? s.replace(/\./g, '-') : s);
  const stripContextTag = (s: string) => s.replace(/\[[^\]]+\]$/, '');

  const raw = model;
  const noProvider = stripProvider(raw);
  const dashed = canonAnthropic(noProvider);
  const noTag = stripContextTag(dashed);

  push(raw);
  push(noProvider);
  push(dashed);
  push(noTag);
  push(stripDate(raw));
  push(stripDate(noProvider));
  push(stripDate(dashed));
  push(stripDate(noTag));
  canonicalCandidatesCache.set(model, out);
  return out;
}

export function isModelPriced(model: string, provider?: string): boolean {
  return resolvePricing(model, provider) !== undefined;
}

export function collectUnmappedModels(rows: readonly Priceable[]): string[] {
  const set = new Set<string>();
  for (const r of rows) {
    if (!r.model || isModelPriced(r.model, r.provider)) continue;
    const uncosted = uncostedTokens(r);
    const needsEstimate =
      uncosted.input > 0 ||
      uncosted.output > 0 ||
      uncosted.cacheRead > 0 ||
      uncosted.cacheWrite > 0;
    if (!needsEstimate && (r.cost_usd_ticks ?? 0) > 0) continue;
    set.add(pricingKey(r.model, r.provider));
  }
  return Array.from(set).toSorted();
}

type Priceable = Pick<
  RuntimeUsage,
  'model' | 'input_tokens' | 'output_tokens' | 'cache_read_tokens' | 'cache_write_tokens'
> & {
  provider?: string;
  cost_usd_ticks?: number;
  uncosted_input_tokens?: number;
  uncosted_output_tokens?: number;
  uncosted_cache_read_tokens?: number;
  uncosted_cache_write_tokens?: number;
};

const COST_USD_TICKS_PER_USD = 10_000_000_000;

function uncostedTokens(usage: Priceable): {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
} {
  if (usage.uncosted_input_tokens === undefined) {
    if ((usage.cost_usd_ticks ?? 0) > 0) {
      return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
    }
    return {
      input: usage.input_tokens,
      output: usage.output_tokens,
      cacheRead: usage.cache_read_tokens,
      cacheWrite: usage.cache_write_tokens,
    };
  }
  return {
    input: usage.uncosted_input_tokens,
    output: usage.uncosted_output_tokens ?? 0,
    cacheRead: usage.uncosted_cache_read_tokens ?? 0,
    cacheWrite: usage.uncosted_cache_write_tokens ?? 0,
  };
}

export function estimateCost(usage: Priceable): number {
  const authoritative = (usage.cost_usd_ticks ?? 0) / COST_USD_TICKS_PER_USD;
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) return authoritative;
  const uncosted = uncostedTokens(usage);
  return (
    authoritative +
    (uncosted.input * pricing.input +
      uncosted.output * pricing.output +
      uncosted.cacheRead * pricing.cacheRead +
      uncosted.cacheWrite * pricing.cacheWrite) /
      1_000_000
  );
}

export interface CostBreakdown {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export function estimateCostBreakdown(usage: Priceable): CostBreakdown {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) {
    return {
      input: (usage.cost_usd_ticks ?? 0) / COST_USD_TICKS_PER_USD,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
  }
  const uncosted = uncostedTokens(usage);
  const breakdown: CostBreakdown = {
    input: (uncosted.input * pricing.input) / 1_000_000,
    output: (uncosted.output * pricing.output) / 1_000_000,
    cacheRead: (uncosted.cacheRead * pricing.cacheRead) / 1_000_000,
    cacheWrite: (uncosted.cacheWrite * pricing.cacheWrite) / 1_000_000,
  };

  const authoritative = (usage.cost_usd_ticks ?? 0) / COST_USD_TICKS_PER_USD;
  if (authoritative <= 0) return breakdown;

  const shape = {
    input: ((usage.input_tokens - uncosted.input) * pricing.input) / 1_000_000,
    output: ((usage.output_tokens - uncosted.output) * pricing.output) / 1_000_000,
    cacheRead: ((usage.cache_read_tokens - uncosted.cacheRead) * pricing.cacheRead) / 1_000_000,
    cacheWrite: ((usage.cache_write_tokens - uncosted.cacheWrite) * pricing.cacheWrite) / 1_000_000,
  };
  const shapeTotal = shape.input + shape.output + shape.cacheRead + shape.cacheWrite;
  if (shapeTotal <= 0) {
    return { ...breakdown, input: breakdown.input + authoritative };
  }
  const scale = authoritative / shapeTotal;
  return {
    input: breakdown.input + shape.input * scale,
    output: breakdown.output + shape.output * scale,
    cacheRead: breakdown.cacheRead + shape.cacheRead * scale,
    cacheWrite: breakdown.cacheWrite + shape.cacheWrite * scale,
  };
}

export function estimateCacheSavings(usage: Priceable): number {
  const pricing = resolvePricing(usage.model, usage.provider);
  if (!pricing) return 0;
  const wouldHaveCost = (usage.cache_read_tokens * pricing.input) / 1_000_000;
  const actualCost = (usage.cache_read_tokens * pricing.cacheRead) / 1_000_000;
  return wouldHaveCost - actualCost;
}

export interface DailyTokenData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface DailyCostData {
  date: string;
  label: string;
  cost: number;
}

export interface DailyCostStackData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export interface ModelDistribution {
  model: string;
  tokens: number;
  cost: number;
}

export interface WeeklyTokenData {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface WeeklyCostStackData {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export function aggregateByDate(usage: RuntimeUsage[]): {
  dailyTokens: DailyTokenData[];
  dailyCost: DailyCostData[];
  dailyCostStack: DailyCostStackData[];
  modelDist: ModelDistribution[];
} {
  const dateMap = new Map<string, Omit<DailyTokenData, 'label'>>();
  const costMap = new Map<string, number>();
  const stackMap = new Map<string, { input: number; output: number; cacheWrite: number }>();
  const modelMap = new Map<string, { tokens: number; cost: number }>();

  for (const u of usage) {
    const existing = dateMap.get(u.date) ?? {
      date: u.date,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
    existing.input += u.input_tokens;
    existing.output += u.output_tokens;
    existing.cacheRead += u.cache_read_tokens;
    existing.cacheWrite += u.cache_write_tokens;
    dateMap.set(u.date, existing);

    const dayCost = (costMap.get(u.date) ?? 0) + estimateCost(u);
    costMap.set(u.date, dayCost);

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(u.date) ?? {
      input: 0,
      output: 0,
      cacheWrite: 0,
    };
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
    stackMap.set(u.date, stack);

    const modelName = modelGroupingKey(u.model, u.provider);
    const m = modelMap.get(modelName) ?? { tokens: 0, cost: 0 };
    m.tokens += u.input_tokens + u.output_tokens + u.cache_read_tokens + u.cache_write_tokens;
    m.cost += estimateCost(u);
    modelMap.set(modelName, m);
  }

  const formatLabel = (d: string) => {
    const date = new Date(d + 'T00:00:00');
    return `${date.getMonth() + 1}/${date.getDate()}`;
  };

  const dailyTokens = Array.from(dateMap.values())
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((d) => ({ ...d, label: formatLabel(d.date) }));

  const dailyCost = Array.from(costMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, cost]) => ({
      date,
      label: formatLabel(date),
      cost: Math.round(cost * 100) / 100,
    }));

  const dailyCostStack = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        date,
        label: formatLabel(date),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
      };
    });

  const modelDist = [...modelMap.entries()]
    .map(([model, data]) => ({ model, ...data }))
    .sort((a, b) => b.tokens - a.tokens);

  return { dailyTokens, dailyCost, dailyCostStack, modelDist };
}

type WeeklyAggregable = Pick<
  RuntimeUsage,
  'date' | 'model' | 'input_tokens' | 'output_tokens' | 'cache_read_tokens' | 'cache_write_tokens'
> & { provider?: string };

export function aggregateByWeek(
  usage: readonly WeeklyAggregable[],
  tz: string,
  weekCount: number,
): {
  weeklyTokens: WeeklyTokenData[];
  weeklyCostStack: WeeklyCostStackData[];
} {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);

  type TokenAgg = Omit<
    WeeklyTokenData,
    'label' | 'rangeLabel' | 'partial' | 'daysCovered' | 'weekEnd'
  >;
  const tokenMap = new Map<string, TokenAgg>();
  const stackMap = new Map<string, { input: number; output: number; cacheWrite: number }>();

  for (let i = 0; i < count; i++) {
    const wkStart = addDaysIso(firstWeekStart, i * 7);
    tokenMap.set(wkStart, {
      weekStart: wkStart,
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    });
    stackMap.set(wkStart, { input: 0, output: 0, cacheWrite: 0 });
  }

  for (const u of usage) {
    const wkStart = weekStartIso(u.date);
    if (wkStart < firstWeekStart || wkStart > currentWeekStart) continue;
    const tokens = tokenMap.get(wkStart);
    if (!tokens) continue;
    tokens.input += u.input_tokens;
    tokens.output += u.output_tokens;
    tokens.cacheRead += u.cache_read_tokens;
    tokens.cacheWrite += u.cache_write_tokens;

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(wkStart);
    if (!stack) continue;
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
  }

  const decorate = (weekStart: string) => {
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    const elapsedDays = Math.min(
      7,
      Math.max(
        1,
        diffDaysIso(weekStart, today < weekStart ? weekStart : today < weekEnd ? today : weekEnd) +
          1,
      ),
    );
    return {
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} – ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsedDays : 7,
    };
  };

  const weeklyTokens: WeeklyTokenData[] = Array.from(tokenMap.values())
    .toSorted((a, b) => a.weekStart.localeCompare(b.weekStart))
    .map((t) => ({ ...t, ...decorate(t.weekStart) }));

  const weeklyCostStack: WeeklyCostStackData[] = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([weekStart, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        ...decorate(weekStart),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
      };
    });

  return { weeklyTokens, weeklyCostStack };
}

export function sliceWindow(
  usage: readonly RuntimeUsage[],
  days: number,
  tz: string,
): { filtered: RuntimeUsage[]; prevFiltered: RuntimeUsage[] } {
  const today = todayIso(tz);
  const isoCurrent = addDaysIso(today, -days);
  const isoPrev = addDaysIso(today, -days * 2);
  return {
    filtered: usage.filter((u) => u.date >= isoCurrent),
    prevFiltered: usage.filter((u) => u.date >= isoPrev && u.date < isoCurrent),
  };
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split('-').map(Number);
  const [y2, m2, d2] = to.split('-').map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

export function todayIso(tz: string): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: tz,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date());
}

export function addDaysIso(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  dt.setUTCDate(dt.getUTCDate() + days);
  return dt.toISOString().slice(0, 10);
}

export function weekStartIso(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  const day = dt.getUTCDay(); 
  const offset = (day + 6) % 7; 
  dt.setUTCDate(dt.getUTCDate() - offset);
  return dt.toISOString().slice(0, 10);
}

export function formatShortDate(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  return dt.toLocaleString('en', {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  });
}

export interface CostByKey {
  key: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

export function aggregateCostByAgent(rows: RuntimeUsageByAgent[]): CostByKey[] {
  const map = new Map<string, CostByKey>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? {
      key: r.agent_id,
      tokens: 0,
      cost: 0,
      taskCount: 0,
    };
    entry.tokens += r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
    map.set(r.agent_id, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

export function aggregateCostByModel(rows: RuntimeUsage[]): CostByKey[] {
  const map = new Map<string, CostByKey>();
  for (const r of rows) {
    const key = modelGroupingKey(r.model, r.provider);
    const entry = map.get(key) ?? { key, tokens: 0, cost: 0, taskCount: 0 };
    entry.tokens += r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    map.set(key, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

export function computeCostInWindow(
  rows: readonly RuntimeUsage[],
  daysBack: number,
  tz: string,
  offsetDays: number = 0,
): number {
  const today = todayIso(tz);
  const isoEnd = addDaysIso(today, -offsetDays);
  const isoStart = addDaysIso(today, -offsetDays - daysBack);
  let total = 0;
  for (const r of rows) {
    if (r.date >= isoStart && r.date < isoEnd) total += estimateCost(r);
  }
  return total;
}

export function pctChange(current: number, previous: number): number | null {
  if (previous <= 0) return null;
  return Math.round(((current - previous) / previous) * 100);
}

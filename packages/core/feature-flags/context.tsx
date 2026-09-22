'use client';

import { createContext, useContext, useMemo, type ReactNode } from 'react';
import type { EvalContext } from './types';
import { FeatureFlagService } from './service';

interface FeatureFlagContextValue {
  service: FeatureFlagService;
  ctx: EvalContext;
}

const FeatureFlagContext = createContext<FeatureFlagContextValue | null>(null);

export interface FeatureFlagsProviderProps {
  service: FeatureFlagService;
  context?: EvalContext;
  children: ReactNode;
}

export function FeatureFlagsProvider({
  service,
  context: ctx = {},
  children,
}: FeatureFlagsProviderProps) {
  const value = useMemo<FeatureFlagContextValue>(() => ({ service, ctx }), [service, ctx]);
  return <FeatureFlagContext.Provider value={value}>{children}</FeatureFlagContext.Provider>;
}

export function useFlag(key: string, defaultValue: boolean): boolean {
  const value = useContext(FeatureFlagContext);
  if (!value) return defaultValue;
  return value.service.isEnabled(key, value.ctx, defaultValue);
}

export function useVariant(key: string, defaultValue: string): string {
  const value = useContext(FeatureFlagContext);
  if (!value) return defaultValue;
  return value.service.variant(key, value.ctx, defaultValue);
}

export function useFeatureFlagService(): FeatureFlagService | null {
  return useContext(FeatureFlagContext)?.service ?? null;
}

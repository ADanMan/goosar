'use client';

import type { ReactNode } from 'react';
import { useDashboardGuard } from './use-dashboard-guard';

interface DashboardGuardProps {
  children: ReactNode;
  loadingFallback?: ReactNode;
}

export function DashboardGuard({ children, loadingFallback = null }: DashboardGuardProps) {
  const { user, isLoading, workspace } = useDashboardGuard();

  if (isLoading || !workspace) return <>{loadingFallback}</>;
  if (!user) return null;

  return <>{children}</>;
}

'use client';

import { Component, type ErrorInfo, type ReactNode } from 'react';
import { Button } from '../ui/button';

export interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: (args: { error: Error; reset: () => void }) => ReactNode;
  onError?: (error: Error, info: ErrorInfo) => void;
  resetKeys?: ReadonlyArray<unknown>;
}

interface ErrorBoundaryState {
  error: Error | null;
}

const INITIAL_STATE: ErrorBoundaryState = { error: null };

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = INITIAL_STATE;

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    this.props.onError?.(error, info);
    console.error('ErrorBoundary caught:', error, info.componentStack);
  }

  override componentDidUpdate(prevProps: ErrorBoundaryProps): void {
    if (this.state.error == null) return;
    const prev = prevProps.resetKeys;
    const next = this.props.resetKeys;
    if (!prev || !next) return;
    if (prev.length !== next.length) {
      this.reset();
      return;
    }
    for (let i = 0; i < prev.length; i++) {
      if (!Object.is(prev[i], next[i])) {
        this.reset();
        return;
      }
    }
  }

  reset = (): void => {
    this.setState(INITIAL_STATE);
  };

  override render(): ReactNode {
    const { error } = this.state;
    if (error == null) return this.props.children;
    if (this.props.fallback) {
      return this.props.fallback({ error, reset: this.reset });
    }
    return <DefaultFallback error={error} reset={this.reset} />;
  }
}

function DefaultFallback({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <div
      role="alert"
      className="flex flex-col items-start gap-3 rounded-md border border-dashed border-border bg-muted/30 p-4 text-sm"
    >
      <div className="space-y-1">
        <p className="font-medium text-foreground">Something went wrong displaying this section.</p>
        <p className="text-muted-foreground">{error.message || 'An unexpected error occurred.'}</p>
      </div>
      <Button size="sm" variant="outline" onClick={reset}>
        Try again
      </Button>
    </div>
  );
}

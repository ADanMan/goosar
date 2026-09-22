import { describe, expect, it } from 'vitest';
import { resolvePreWorkspaceGate } from './pre-workspace-gate';

describe('resolvePreWorkspaceGate', () => {
  it('admits an onboarded user who has a workspace', () => {
    expect(
      resolvePreWorkspaceGate({
        hasOnboarded: true,
        workspaceCount: 1,
        hasPendingCompletion: false,
      }),
    ).toBe('dashboard');
  });

  it('sends an onboarded user with no workspace to workspace creation', () => {
    expect(
      resolvePreWorkspaceGate({
        hasOnboarded: true,
        workspaceCount: 0,
        hasPendingCompletion: false,
      }),
    ).toBe('new-workspace');
  });

  it('looks up invitations for a genuinely new user', () => {
    expect(
      resolvePreWorkspaceGate({
        hasOnboarded: false,
        workspaceCount: 0,
        hasPendingCompletion: false,
      }),
    ).toBe('check-invitations');
  });

  it('sends a user with an undelivered completion straight to onboarding', () => {
    expect(
      resolvePreWorkspaceGate({
        hasOnboarded: false,
        workspaceCount: 0,
        hasPendingCompletion: true,
      }),
    ).toBe('onboarding');
  });

  it('never admits to the dashboard on the local mark alone', () => {
    expect(
      resolvePreWorkspaceGate({
        hasOnboarded: false,
        workspaceCount: 7,
        hasPendingCompletion: true,
      }),
    ).toBe('onboarding');
  });
});

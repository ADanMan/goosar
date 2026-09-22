import type { MemberRole } from '@goosar/core/types';

export type MembershipGate =
  { kind: 'noop' } | { kind: 'confirm' } | { kind: 'typed'; target: 'member' | 'workspace' };

export function roleChangeGate(from: MemberRole, to: MemberRole): MembershipGate {
  if (from === to) return { kind: 'noop' };
  if (to === 'owner') return { kind: 'typed', target: 'workspace' };
  if (from === 'owner') return { kind: 'typed', target: 'member' };
  if (to === 'admin') return { kind: 'typed', target: 'member' };
  return { kind: 'confirm' };
}

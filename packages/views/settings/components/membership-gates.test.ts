import { describe, expect, it } from 'vitest';
import type { MemberRole } from '@goosar/core/types';
import { roleChangeGate } from './membership-gates';

const ROLES: MemberRole[] = ['owner', 'admin', 'member'];

describe('roleChangeGate (issue #248, §3 classes)', () => {
  it('asks for nothing when the picked role is the one already held', () => {
    for (const role of ROLES) {
      expect(roleChangeGate(role, role)).toEqual({ kind: 'noop' });
    }
  });

  it('types the WORKSPACE name to hand out ownership', () => {
    expect(roleChangeGate('member', 'owner')).toEqual({
      kind: 'typed',
      target: 'workspace',
    });
    expect(roleChangeGate('admin', 'owner')).toEqual({
      kind: 'typed',
      target: 'workspace',
    });
  });

  it('types the MEMBER name to promote a member to admin', () => {
    expect(roleChangeGate('member', 'admin')).toEqual({
      kind: 'typed',
      target: 'member',
    });
  });

  it('types the MEMBER name to take ownership away', () => {
    expect(roleChangeGate('owner', 'admin')).toEqual({
      kind: 'typed',
      target: 'member',
    });
    expect(roleChangeGate('owner', 'member')).toEqual({
      kind: 'typed',
      target: 'member',
    });
  });

  it('asks a plain confirmation to demote an admin to member', () => {
    expect(roleChangeGate('admin', 'member')).toEqual({ kind: 'confirm' });
  });

  it('never leaves a real role change unconfirmed', () => {
    for (const from of ROLES) {
      for (const to of ROLES) {
        const gate = roleChangeGate(from, to);
        if (from === to) continue;
        expect(gate.kind).not.toBe('noop');
      }
    }
  });
});

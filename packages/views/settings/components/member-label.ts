import type { MemberWithUser } from '@goosar/core/types';

export function memberLabel(member: Pick<MemberWithUser, 'name' | 'email'>): string {
  const name = member.name.trim();
  return name !== '' ? name : member.email.trim();
}

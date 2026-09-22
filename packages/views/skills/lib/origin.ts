import type { Skill, SkillSummary } from '@goosar/core/types';

export type OriginInfo = {
  type: 'runtime_local' | 'clawhub' | 'skills_sh' | 'github' | 'manual';
  provider?: string;
  runtime_id?: string;
  source_path?: string;
  source_url?: string;
};

export function readOrigin(skill: SkillSummary): OriginInfo {
  const raw = (skill.config?.origin ?? null) as (OriginInfo & Record<string, unknown>) | null;
  if (raw?.type === 'runtime_local') return raw;
  if (raw?.type === 'clawhub') return raw;
  if (raw?.type === 'skills_sh') return raw;
  if (raw?.type === 'github') return raw;
  return { type: 'manual' };
}

export function totalFileCount(skill: Skill | SkillSummary): number {
  const files = (skill as Skill).files;
  return (files?.length ?? 0) + 1;
}

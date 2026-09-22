import type { User } from '../types';
import type { QuestionnaireAnswers } from './types';

export const SOURCE_BACKFILL_MAX_DISMISSALS = 3;

export const SOURCE_BACKFILL_MIN_AGENT_DONE_ISSUES = 3;

export function needsSourceBackfill(user: User | null | undefined, dismissCount: number): boolean {
  if (!user) return false;
  if (!user.onboarded_at) return false;
  if (dismissCount >= SOURCE_BACKFILL_MAX_DISMISSALS) return false;

  const q = user.onboarding_questionnaire as Partial<QuestionnaireAnswers> | null | undefined;
  if (!q) return true;
  if (q.source_skipped === true) return false;
  const raw: unknown = q.source;
  if (Array.isArray(raw)) return raw.length === 0;
  if (typeof raw === 'string') return raw.length === 0;
  return true;
}

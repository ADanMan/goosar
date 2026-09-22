import { api } from '../api';
import { useAuthStore } from '../auth';
import { setPersonProperties } from '../analytics';
import type { OnboardingCompletionPath, QuestionnaireAnswers } from './types';
import { clearOnboardingCompletionMark, markOnboardingCompletionPending } from './completion-mark';

export async function saveQuestionnaire(answers: Partial<QuestionnaireAnswers>): Promise<void> {
  const user = await api.patchOnboarding({ questionnaire: answers });
  useAuthStore.getState().setUser(user);
  const sourceList = answers.source ?? [];
  const useCaseList = answers.use_case ?? [];
  if (sourceList.length > 0 || answers.role || useCaseList.length > 0) {
    setPersonProperties({
      ...(sourceList.length > 0 ? { source: sourceList } : {}),
      ...(answers.role ? { role: answers.role } : {}),
      ...(useCaseList.length > 0 ? { use_case: useCaseList } : {}),
    });
  }
}

export async function completeOnboarding(
  completionPath?: OnboardingCompletionPath,
  workspaceId?: string,
): Promise<void> {
  const user = useAuthStore.getState().user;
  const userId = user?.id ?? null;
  if (user?.onboarded_at == null) {
    markOnboardingCompletionPending(userId);
  }
  await api.markOnboardingComplete(
    completionPath || workspaceId
      ? { completion_path: completionPath, workspace_id: workspaceId }
      : undefined,
  );
  await useAuthStore.getState().refreshMe();
  clearMarkIfServerConfirmed(userId);
}

function clearMarkIfServerConfirmed(userId: string | null): boolean {
  const confirmed = useAuthStore.getState().user?.onboarded_at != null;
  if (confirmed) clearOnboardingCompletionMark(userId);
  return confirmed;
}

export type CompletionDeliveryResult =
  | 'confirmed'
  /** The request went through and the server still reports no `onboarded_at`. */
  | 'not_confirmed'
  /** The request went through; reading the result back failed. */
  | 'delivered_unverified';

export async function retryOnboardingCompletionDelivery(): Promise<CompletionDeliveryResult> {
  const userId = useAuthStore.getState().user?.id ?? null;
  try {
    await useAuthStore.getState().refreshMe();
    if (clearMarkIfServerConfirmed(userId)) return 'confirmed';
  } catch {
    // Fall through to the write.
  }

  await api.markOnboardingComplete();
  try {
    await useAuthStore.getState().refreshMe();
  } catch {
    return 'delivered_unverified';
  }
  return clearMarkIfServerConfirmed(userId) ? 'confirmed' : 'not_confirmed';
}

export async function reconcileOnboardingCompletion(): Promise<boolean> {
  const userId = useAuthStore.getState().user?.id ?? null;
  await useAuthStore.getState().refreshMe();
  return clearMarkIfServerConfirmed(userId);
}

export async function joinCloudWaitlist(email: string, reason: string): Promise<void> {
  await api.joinCloudWaitlist({ email, reason });
}

'use client';

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { AlertCircle, Loader2 } from 'lucide-react';
import { api } from '@goosar/core/api';
import { useAuthStore } from '@goosar/core/auth';
import { useWelcomeStore } from '@goosar/core/onboarding';
import { paths, useCurrentWorkspace } from '@goosar/core/paths';
import { resolvePublicFileUrl } from '@goosar/core/workspace/avatar-url';
import { issueKeys } from '@goosar/core/issues/queries';
import { workspaceKeys } from '@goosar/core/workspace/queries';
import type { Agent, CreateIssueRequest, Issue } from '@goosar/core/types';
import { prepareWorkspaceHelper, HELPER_AVATAR_URL } from './helper-setup';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@goosar/ui/components/ui/dialog';
import { Button } from '@goosar/ui/components/ui/button';
import { cn } from '@goosar/ui/lib/utils';
import { useNavigation } from '../navigation';
import { useT } from '../i18n';
import {
  buildUserContextSection,
  CREATE_AGENT_GUIDE_ISSUE_TITLE,
  FOLLOWUP_COMMENT_PREFIX,
  getCreateAgentGuideBody,
  HELPER_STARTER_PROMPTS,
  INSTALL_RUNTIME_ISSUE_BODY,
  INSTALL_RUNTIME_ISSUE_TITLE,
  ONBOARDING_SEED,
  ONBOARDING_SEED_METADATA_KEY,
  pickContentLang,
  STARTER_CARD_IDS,
  type OnboardingSeedSlug,
  type StarterCardId,
  type UserContextLabels,
} from '../onboarding/templates';

export function WelcomeAfterOnboarding() {
  const me = useAuthStore((s) => s.user);
  const currentWorkspace = useCurrentWorkspace();
  const signal = useWelcomeStore((s) => s.signal);
  const dismissed = useWelcomeStore((s) => s.dismissed);
  const dismiss = useWelcomeStore((s) => s.dismiss);

  if (
    !me ||
    !signal ||
    dismissed ||
    !currentWorkspace ||
    currentWorkspace.id !== signal.workspaceId
  ) {
    return null;
  }

  if (signal.choice === 'runtime' && signal.runtimeId) {
    return (
      <RuntimeWelcome
        workspaceId={signal.workspaceId}
        runtimeId={signal.runtimeId}
        onAbandon={dismiss}
        onComplete={dismiss}
      />
    );
  }
  if (signal.choice === 'skip') {
    return <SkipWelcome workspaceId={signal.workspaceId} onDismiss={dismiss} />;
  }
  return null;
}

const pendingIssueSeed = new Map<string, Promise<Issue>>();

function seedIssueDeduped(
  cacheKey: string,
  seedSlug: OnboardingSeedSlug,
  body: CreateIssueRequest,
): Promise<Issue> {
  const existing = pendingIssueSeed.get(cacheKey);
  if (existing) return existing;
  const promise = (async (): Promise<Issue> => {
    const issue = await api.createIssue(body);
    try {
      await api.setIssueMetadataKey(issue.id, ONBOARDING_SEED_METADATA_KEY, seedSlug);
    } catch {
      // Swallow: the guide exists; the gate's title fallback still works.
    }
    return issue;
  })();
  pendingIssueSeed.set(cacheKey, promise);
  promise
    .finally(() => {
      if (pendingIssueSeed.get(cacheKey) === promise) {
        pendingIssueSeed.delete(cacheKey);
      }
    })
    .catch(() => {});
  return promise;
}

const pendingCommentSeed = new Map<string, Promise<unknown>>();

function postCommentDeduped(cacheKey: string, issueId: string, content: string): Promise<unknown> {
  const existing = pendingCommentSeed.get(cacheKey);
  if (existing) return existing;
  const promise = api.createComment(issueId, content);
  pendingCommentSeed.set(cacheKey, promise);
  promise
    .finally(() => {
      if (pendingCommentSeed.get(cacheKey) === promise) {
        pendingCommentSeed.delete(cacheKey);
      }
    })
    .catch(() => {});
  return promise;
}

interface RuntimeWelcomeProps {
  workspaceId: string;
  runtimeId: string;
  onAbandon: () => void;
  onComplete: () => void;
}

function RuntimeWelcome({ workspaceId, runtimeId, onAbandon, onComplete }: RuntimeWelcomeProps) {
  const { t, i18n } = useT('onboarding');
  const navigation = useNavigation();
  const qc = useQueryClient();
  const me = useAuthStore((s) => s.user);

  const [agent, setAgent] = useState<Agent | null>(null);
  const [prepError, setPrepError] = useState<Error | null>(null);
  const [attemptKey, setAttemptKey] = useState(0);

  const [selected, setSelected] = useState<Set<StarterCardId>>(() => new Set());
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const submitInFlightRef = useRef(false);
  const [successIssueId, setSuccessIssueId] = useState<string | null>(null);

  const userContextLabels: UserContextLabels = useMemo(() => {
    const lang = pickContentLang(i18n.language);
    return {
      heading: t(($) => $.welcome_after_onboarding.user_context_heading),
      roleLabel: t(($) => $.welcome_after_onboarding.user_context_role_label),
      useCaseLabel: t(($) => $.welcome_after_onboarding.user_context_use_case_label),
      listSeparator: lang === 'zh' || lang === 'ja' ? '、' : ', ',
      role: {
        engineer: t(($) => $.questions.role.engineer),
        product: t(($) => $.questions.role.product),
        designer: t(($) => $.questions.role.designer),
        founder: t(($) => $.questions.role.founder),
        marketing: t(($) => $.questions.role.marketing),
        writer: t(($) => $.questions.role.writer),
        research: t(($) => $.questions.role.research),
        ops: t(($) => $.questions.role.ops),
        student: t(($) => $.questions.role.student),
        other: t(($) => $.questions.role.other),
      },
      useCase: {
        ship_code: t(($) => $.questions.use_case.ship_code),
        manage_team: t(($) => $.questions.use_case.manage_team),
        personal_tasks: t(($) => $.questions.use_case.personal_tasks),
        plan_research: t(($) => $.questions.use_case.plan_research),
        write_publish: t(($) => $.questions.use_case.write_publish),
        automate_ops: t(($) => $.questions.use_case.automate_ops),
        evaluate: t(($) => $.questions.use_case.evaluate),
        other: t(($) => $.questions.use_case.other),
      },
    };
  }, [t, i18n.language]);
  const toggle = (id: StarterCardId) => {
    if (submitting) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  useEffect(() => {
    if (agent) return;
    let cancelled = false;
    (async () => {
      const found = await prepareWorkspaceHelper(workspaceId, runtimeId);
      if (cancelled) return;
      if (!found) {
        setPrepError(new Error(''));
        return;
      }
      setAgent(found);
      qc.invalidateQueries({
        queryKey: workspaceKeys.agents(workspaceId),
      });
    })();
    return () => {
      cancelled = true;
    };
  }, [agent, attemptKey, qc, runtimeId, workspaceId]);

  if (prepError) {
    return (
      <FullScreenError
        title={t(($) => $.welcome_after_onboarding.error_title)}
        message={prepError.message || t(($) => $.welcome_after_onboarding.error_generic)}
        retryLabel={t(($) => $.welcome_after_onboarding.retry)}
        closeLabel={t(($) => $.welcome_after_onboarding.error_close)}
        onRetry={() => {
          setPrepError(null);
          setAttemptKey((n) => n + 1);
        }}
        onClose={onAbandon}
      />
    );
  }

  if (!agent) {
    return <FullScreenLoading label={t(($) => $.welcome_after_onboarding.loading_helper)} />;
  }

  const lang = pickContentLang(i18n.language);

  const handleAssign = async () => {
    if (submitInFlightRef.current || selected.size === 0) return;
    submitInFlightRef.current = true;
    setSubmitError(null);
    setSubmitting(true);
    try {
      const userContext = buildUserContextSection(me?.onboarding_questionnaire, userContextLabels);
      const orderedIds = STARTER_CARD_IDS.filter((id) => selected.has(id));
      const issues = await Promise.all(
        orderedIds.map((id) => {
          const card = HELPER_STARTER_PROMPTS[id];
          return api.createIssue({
            title: card.title[lang],
            description: card.prompt[lang] + userContext,
            status: 'todo',
            priority: 'high',
            assignee_type: 'agent',
            assignee_id: agent.id,
          });
        }),
      );
      await Promise.all([
        qc.invalidateQueries({ queryKey: issueKeys.all(workspaceId) }),
        qc.invalidateQueries({
          queryKey: workspaceKeys.agents(workspaceId),
        }),
      ]);
      submitInFlightRef.current = false;
      setSubmitting(false);
      setSuccessIssueId(issues[0]!.id);
    } catch (err) {
      submitInFlightRef.current = false;
      setSubmitting(false);
      setSubmitError(
        err instanceof Error ? err.message : t(($) => $.welcome_after_onboarding.error_generic),
      );
    }
  };

  const handleGotIt = async () => {
    if (!successIssueId) return;
    onComplete();
    const slug = await resolveWorkspaceSlug(qc, workspaceId);
    navigation.push(paths.workspace(slug).issueDetail(successIssueId));
  };

  if (successIssueId) {
    return (
      <Dialog
        open={true}
        modal={true}
        disablePointerDismissal={true}
        onOpenChange={() => {
          /* blocking until Got it */
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="max-w-md sm:max-w-md"
          aria-describedby="welcome-after-onboarding-runtime-success-subtitle"
        >
          <div className="flex flex-col items-center gap-3 pt-4">
            <div className="text-4xl animate-welcome-emoji-pop" aria-hidden>
              🎉
            </div>
            <DialogTitle className="text-center text-2xl font-semibold">
              {t(($) => $.welcome_after_onboarding.runtime.success.title)}
            </DialogTitle>
            <DialogDescription
              id="welcome-after-onboarding-runtime-success-subtitle"
              className="text-center text-sm text-muted-foreground"
            >
              {t(($) => $.welcome_after_onboarding.runtime.success.subtitle, {
                agentName: agent.name,
              })}
            </DialogDescription>
            <div className="mt-1 flex flex-col gap-1.5 max-w-sm">
              <p className="text-center text-xs text-muted-foreground/80 leading-relaxed">
                {t(($) => $.welcome_after_onboarding.runtime.success.tip_inbox)}
              </p>
              <p className="text-center text-xs text-muted-foreground/80 leading-relaxed">
                {t(($) => $.welcome_after_onboarding.runtime.success.tip_chat)}
              </p>
            </div>
          </div>
          <div className="mt-4 flex justify-end">
            <Button size="lg" onClick={handleGotIt}>
              {t(($) => $.welcome_after_onboarding.runtime.success.got_it)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog
      open={true}
      modal={true}
      disablePointerDismissal={true}
      onOpenChange={() => {
        /* runtime path is blocking by design */
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="max-w-xl sm:max-w-xl"
        aria-describedby="welcome-after-onboarding-runtime-subtitle"
      >
        <div className="flex flex-col items-center gap-3 pt-4 animate-onboarding-enter">
          <img
            src={resolvePublicFileUrl(agent.avatar_url) ?? HELPER_AVATAR_URL}
            alt=""
            aria-hidden
            className="h-14 w-14 rounded-xl ring-1 ring-foreground/10"
          />
          <DialogTitle className="text-center text-xl font-semibold">
            {t(($) => $.welcome_after_onboarding.runtime.greeting)}
          </DialogTitle>
          <DialogDescription
            id="welcome-after-onboarding-runtime-subtitle"
            className="text-center text-sm text-muted-foreground"
          >
            {t(($) => $.welcome_after_onboarding.runtime.subtitle)}
          </DialogDescription>
          <p className="text-center text-sm text-muted-foreground max-w-md leading-relaxed">
            {t(($) => $.welcome_after_onboarding.runtime.capabilities)}
          </p>
        </div>

        <div className="mt-4 border-t pt-4">
          <p className="mb-3 text-sm font-medium text-foreground">
            {t(($) => $.welcome_after_onboarding.runtime.section_label)}
          </p>
          <div className="flex flex-col gap-2">
            {STARTER_CARD_IDS.map((id) => {
              const isSelected = selected.has(id);
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => toggle(id)}
                  disabled={submitting}
                  aria-pressed={isSelected}
                  className={cn(
                    'flex items-start gap-3 rounded-lg border bg-card px-3 py-2.5 text-left transition-colors',
                    isSelected
                      ? 'border-primary ring-1 ring-primary'
                      : 'hover:border-foreground/20',
                    'disabled:cursor-not-allowed disabled:opacity-60',
                  )}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium leading-tight">
                      {HELPER_STARTER_PROMPTS[id].title[lang]}
                    </p>
                    <p className="mt-0.5 text-xs text-muted-foreground leading-snug">
                      {t(($) => $.welcome_after_onboarding.runtime.cards[id].subtitle)}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>

          <div className="mt-4 flex justify-end">
            <Button size="lg" disabled={selected.size === 0 || submitting} onClick={handleAssign}>
              {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
              {selected.size === 0
                ? t(($) => $.welcome_after_onboarding.runtime.assign_empty)
                : t(($) => $.welcome_after_onboarding.runtime.assign_count, {
                    count: selected.size,
                  })}
            </Button>
          </div>
        </div>

        {submitError ? (
          <div
            role="alert"
            className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            <p>{submitError}</p>
            <Button
              variant="ghost"
              size="sm"
              className="mt-1 h-6 px-2 text-xs"
              onClick={() => setSubmitError(null)}
            >
              {t(($) => $.welcome_after_onboarding.dismiss_error)}
            </Button>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

type SkipBundle =
  | { kind: 'helper'; agent: Agent }
  | { kind: 'guides'; installIssueId: string; agentGuideId: string };

interface SkipWelcomeProps {
  workspaceId: string;
  onDismiss: () => void;
}

function SkipWelcome({ workspaceId, onDismiss }: SkipWelcomeProps) {
  const { t, i18n } = useT('onboarding');
  const navigation = useNavigation();
  const qc = useQueryClient();
  const me = useAuthStore((s) => s.user);

  const [bundle, setBundle] = useState<SkipBundle | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!me) return;
    if (bundle || failed) return;
    let cancelled = false;
    (async () => {
      try {
        const lang = pickContentLang(i18n.language);
        const helper = await prepareWorkspaceHelper(workspaceId, null);
        if (helper) {
          qc.invalidateQueries({
            queryKey: workspaceKeys.agents(workspaceId),
          });
          if (!cancelled) setBundle({ kind: 'helper', agent: helper });
          return;
        }
        const installRuntime = await seedIssueDeduped(
          `${workspaceId}:install-runtime`,
          ONBOARDING_SEED.installRuntime,
          {
            title: INSTALL_RUNTIME_ISSUE_TITLE[lang],
            description: INSTALL_RUNTIME_ISSUE_BODY[lang],
            status: 'in_progress',
            priority: 'high',
            assignee_type: 'member',
            assignee_id: me.id,
          },
        );
        const agentGuide = await seedIssueDeduped(
          `${workspaceId}:create-agent-guide`,
          ONBOARDING_SEED.createAgentGuide,
          {
            title: CREATE_AGENT_GUIDE_ISSUE_TITLE[lang],
            description: getCreateAgentGuideBody({
              lang,
              installRuntimeIdentifier: installRuntime.identifier,
              installRuntimeId: installRuntime.id,
            }),
            status: 'todo',
            priority: 'medium',
            assignee_type: 'member',
            assignee_id: me.id,
          },
        );
        const prefix = FOLLOWUP_COMMENT_PREFIX[lang];
        const commentText = `${prefix} [${agentGuide.identifier}](mention://issue/${agentGuide.id})`;
        await postCommentDeduped(
          `${workspaceId}:install-runtime-followup`,
          installRuntime.id,
          commentText,
        );
        qc.invalidateQueries({ queryKey: issueKeys.all(workspaceId) });
        if (!cancelled) {
          setBundle({
            kind: 'guides',
            installIssueId: installRuntime.id,
            agentGuideId: agentGuide.id,
          });
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [bundle, failed, i18n.language, me, qc, t, workspaceId]);

  useEffect(() => {
    if (failed) onDismiss();
  }, [failed, onDismiss]);

  if (failed || !me) return null;

  if (!bundle) {
    return <FullScreenLoading label={t(($) => $.welcome_after_onboarding.skip.loading)} />;
  }

  const handleGotIt = async () => {
    onDismiss();
    const slug = await resolveWorkspaceSlug(qc, workspaceId);
    navigation.push(
      bundle.kind === 'helper'
        ? paths.workspace(slug).agentDetail(bundle.agent.id)
        : paths.workspace(slug).issueDetail(bundle.installIssueId),
    );
  };

  return (
    <Dialog
      open={true}
      modal={true}
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <DialogContent
        className="max-w-xl sm:max-w-xl"
        aria-describedby="welcome-after-onboarding-skip-subtitle"
      >
        <div className="flex flex-col items-center gap-4 pt-6">
          <div className="text-6xl animate-welcome-emoji-pop" aria-hidden>
            🎉
          </div>
          <DialogTitle className="text-center text-2xl font-semibold">
            {t(($) => $.welcome_after_onboarding.skip.title)}
          </DialogTitle>
          <DialogDescription
            id="welcome-after-onboarding-skip-subtitle"
            className="text-center text-sm text-muted-foreground max-w-md"
          >
            {bundle.kind === 'helper'
              ? t(($) => $.welcome_after_onboarding.skip.subtitle_helper)
              : t(($) => $.welcome_after_onboarding.skip.subtitle)}
          </DialogDescription>
        </div>

        <div className="mt-6 flex flex-col gap-2">
          {bundle.kind === 'helper' ? (
            <SkipPreviewCard
              title={bundle.agent.name}
              subtitle={t(($) => $.welcome_after_onboarding.skip.cards.helper_ready.subtitle)}
              statusLabel={t(($) => $.welcome_after_onboarding.skip.status_ready)}
              statusTone="active"
              leading={
                <img
                  src={resolvePublicFileUrl(bundle.agent.avatar_url) ?? HELPER_AVATAR_URL}
                  alt=""
                  aria-hidden
                  className="mt-0.5 h-8 w-8 shrink-0 rounded-lg ring-1 ring-foreground/10"
                />
              }
            />
          ) : (
            <>
              <SkipPreviewCard
                title={t(($) => $.welcome_after_onboarding.skip.cards.install_runtime.title)}
                subtitle={t(($) => $.welcome_after_onboarding.skip.cards.install_runtime.subtitle)}
                statusLabel={t(($) => $.welcome_after_onboarding.skip.status_in_progress)}
                statusTone="active"
              />
              <SkipPreviewCard
                title={t(($) => $.welcome_after_onboarding.skip.cards.create_agent.title)}
                subtitle={t(($) => $.welcome_after_onboarding.skip.cards.create_agent.subtitle)}
                statusLabel={t(($) => $.welcome_after_onboarding.skip.status_todo)}
                statusTone="todo"
              />
            </>
          )}
        </div>

        <div className="mt-6 flex justify-end">
          <Button size="lg" onClick={handleGotIt}>
            {t(($) => $.welcome_after_onboarding.skip.got_it)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SkipPreviewCard({
  title,
  subtitle,
  statusLabel,
  statusTone,
  leading,
}: {
  title: string;
  subtitle: string;
  statusLabel: string;
  statusTone: 'active' | 'todo';
  leading?: ReactNode;
}) {
  return (
    <div className="flex items-start gap-3 rounded-lg border bg-background px-3 py-2.5">
      {leading}
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm font-medium leading-tight">{title}</p>
          <span
            className={cn(
              'rounded-full px-2 py-0.5 text-[11px] font-medium',
              statusTone === 'active'
                ? 'bg-primary/10 text-primary'
                : 'bg-muted text-muted-foreground',
            )}
          >
            {statusLabel}
          </span>
        </div>
        <p className="mt-1 text-xs text-muted-foreground leading-snug">{subtitle}</p>
      </div>
    </div>
  );
}

function FullScreenLoading({ label }: { label: string }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="flex flex-col items-center gap-3">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <p className="text-sm text-muted-foreground">{label}</p>
      </div>
    </div>
  );
}

function FullScreenError({
  title,
  message,
  retryLabel,
  closeLabel,
  onRetry,
  onClose,
}: {
  title: string;
  message: string;
  retryLabel: string;
  closeLabel: string;
  onRetry: () => void;
  onClose: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="flex max-w-md flex-col items-center gap-4 rounded-lg border bg-card p-6 shadow-md">
        <AlertCircle className="h-6 w-6 text-destructive" />
        <p className="text-center text-sm font-medium text-foreground">{title}</p>
        <p className="text-center text-xs text-muted-foreground">{message}</p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={onClose}>
            {closeLabel}
          </Button>
          <Button size="sm" onClick={onRetry}>
            {retryLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

async function resolveWorkspaceSlug(
  qc: ReturnType<typeof useQueryClient>,
  workspaceId: string,
): Promise<string> {
  const cached = qc
    .getQueriesData<{ id: string; slug: string }[] | undefined>({
      queryKey: workspaceKeys.list(),
    })
    .map(([, data]) => data)
    .find(Boolean);
  const hit = cached?.find((w) => w.id === workspaceId);
  if (hit) return hit.slug;
  const ws = await api.getWorkspace(workspaceId);
  return ws.slug;
}

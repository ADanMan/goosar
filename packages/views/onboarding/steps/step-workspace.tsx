'use client';

import { type ReactNode, useRef, useState } from 'react';
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  FolderKanban,
  Inbox,
  ListTodo,
  Lock,
  Monitor,
  Plus,
  Zap,
} from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@goosar/ui/components/ui/button';
import { Input } from '@goosar/ui/components/ui/input';
import { Label } from '@goosar/ui/components/ui/label';
import { useScrollFade } from '@goosar/ui/hooks/use-scroll-fade';
import { cn } from '@goosar/ui/lib/utils';
import { useCreateWorkspace } from '@goosar/core/workspace/mutations';
import type { Workspace } from '@goosar/core/types';
import { isImeComposing } from '@goosar/core/utils';
import { useConfigStore } from '@goosar/core/config';
import { workspaceUrlHost } from '@goosar/core/workspace/workspace-url';
import { DragStrip } from '@goosar/views/platform';
import { useLogout } from '../../auth';
import { StepHeader } from '../components/step-header';
import { RadioMark } from '../components/option-card';
import { WorkspaceAvatar } from '../../workspace/workspace-avatar';
import { useT } from '../../i18n';
import {
  WORKSPACE_SLUG_REGEX,
  isWorkspaceSlugConflict,
  nameToWorkspaceSlug,
} from '../../workspace/slug';
import { isReservedSlug } from '@goosar/core/paths';

function issuePrefix(slug: string): string {
  const head = slug
    .trim()
    .replace(/[^a-z0-9]/g, '')
    .slice(0, 4);
  return (head || 'ws').toUpperCase();
}

export function StepWorkspace({
  existing,
  onCreated,
  onBack,
}: {
  existing?: Workspace | null;
  onCreated: (workspace: Workspace) => void | Promise<void>;
  onBack?: () => void;
}) {
  const { t } = useT('onboarding');
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);
  const workspaceCreationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  const workspaceCreationAllowed = !workspaceCreationDisabled;
  const logout = useLogout();

  const reusing = existing ?? null;
  const [mode, setMode] = useState<'existing' | 'create' | null>(() =>
    !workspaceCreationAllowed && existing ? 'existing' : null,
  );
  const pickExisting = () => setMode((m) => (m === 'existing' ? null : 'existing'));
  const pickCreate = () => setMode((m) => (m === 'create' ? null : 'create'));

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugServerError, setSlugServerError] = useState<string | null>(null);
  const slugTouched = useRef(false);

  const slugValidationError =
    slug.length > 0 && !WORKSPACE_SLUG_REGEX.test(slug)
      ? t(($) => $.step_workspace.slug_format_error)
      : null;
  const slugReservedError =
    slug.length > 0 && isReservedSlug(slug) ? t(($) => $.step_workspace.slug_reserved_error) : null;
  const slugError = slugValidationError ?? slugReservedError ?? slugServerError;
  const canCreate = name.trim().length > 0 && slug.trim().length > 0 && !slugError;

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugTouched.current) {
      setSlug(nameToWorkspaceSlug(value));
      setSlugServerError(null);
    }
  };

  const handleSlugChange = (value: string) => {
    slugTouched.current = true;
    setSlug(value);
    setSlugServerError(null);
  };

  const createWorkspace = useCreateWorkspace();

  const handleCreate = () => {
    if (!canCreate || createWorkspace.isPending) return;
    createWorkspace.mutate(
      { name: name.trim(), slug: slug.trim() },
      {
        onSuccess: onCreated,
        onError: (error) => {
          if (isWorkspaceSlugConflict(error)) {
            setSlugServerError(t(($) => $.step_workspace.slug_taken_error));
            toast.error(t(($) => $.step_workspace.slug_conflict_toast));
            return;
          }
          toast.error(
            error instanceof Error && error.message
              ? error.message
              : t(($) => $.step_workspace.create_failed_toast),
          );
        },
      },
    );
  };

  const isCreating = createWorkspace.isPending;
  const creatingActive = workspaceCreationAllowed && (!reusing || mode === 'create');
  const existingActive = Boolean(reusing) && mode === 'existing';

  let hint: string;
  let continueLabel: string;
  let continueDisabled: boolean;
  let onContinue: () => void;

  if (existingActive && reusing) {
    hint = t(($) => $.step_workspace.hint_opening, { name: reusing.name });
    continueLabel = t(($) => $.step_workspace.cta_open, { name: reusing.name });
    continueDisabled = isCreating;
    onContinue = () => onCreated(reusing);
  } else if (creatingActive) {
    if (isCreating) {
      hint = t(($) => $.step_workspace.hint_creating_pending, {
        name: name.trim() || t(($) => $.step_workspace.hint_creating_fallback),
      });
      continueLabel = t(($) => $.step_workspace.cta_creating);
      continueDisabled = true;
      onContinue = () => {};
    } else if (canCreate) {
      hint = t(($) => $.step_workspace.hint_creating, { name: name.trim() });
      continueLabel = t(($) => $.step_workspace.cta_create_named, { name: name.trim() });
      continueDisabled = false;
      onContinue = handleCreate;
    } else {
      hint = t(($) => $.step_workspace.hint_name_first);
      continueLabel = t(($) => $.step_workspace.cta_create_workspace);
      continueDisabled = true;
      onContinue = () => {};
    }
  } else {
    hint = t(($) => $.step_workspace.hint_pick);
    continueLabel = t(($) => $.common.continue);
    continueDisabled = true;
    onContinue = () => {};
  }

  const createFields = (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ws-name" className="text-xs font-medium text-muted-foreground">
          {t(($) => $.step_workspace.name_label)}
        </Label>
        <Input
          id="ws-name"
          autoFocus
          type="text"
          value={name}
          onChange={(e) => handleNameChange(e.target.value)}
          placeholder={t(($) => $.step_workspace.name_placeholder)}
          onKeyDown={(e) => {
            if (isImeComposing(e)) return;
            if (e.key === 'Enter') handleCreate();
          }}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="ws-slug" className="text-xs font-medium text-muted-foreground">
          {t(($) => $.step_workspace.url_label)}
        </Label>
        <div className="flex items-center rounded-md border bg-muted transition-colors focus-within:border-foreground">
          <span className="select-none pl-3 font-mono text-sm text-muted-foreground">
            {`${urlHost}/`}
          </span>
          <Input
            id="ws-slug"
            type="text"
            value={slug}
            onChange={(e) => handleSlugChange(e.target.value)}
            placeholder={t(($) => $.step_workspace.slug_placeholder)}
            className="border-0 bg-transparent font-mono shadow-none focus-visible:ring-0"
            onKeyDown={(e) => {
              if (isImeComposing(e)) return;
              if (e.key === 'Enter') handleCreate();
            }}
          />
        </div>
        {slugError && <p className="text-xs text-destructive">{slugError}</p>}
      </div>
      <div className="flex flex-col gap-1.5">
        <div className="text-xs font-medium text-muted-foreground">
          {t(($) => $.step_workspace.issue_prefix_label)}
        </div>
        <div className="text-sm leading-[1.55] text-muted-foreground">
          {t(($) => $.step_workspace.issue_prefix_prefix)}
          <span className="font-mono text-foreground">{issuePrefix(slug)}-123</span>
          {t(($) => $.step_workspace.issue_prefix_suffix)}
        </div>
      </div>
    </div>
  );

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left column — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              disabled={isCreating}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground disabled:opacity-40"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t(($) => $.common.back)}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="workspace" />
          </div>
        </header>

        <main ref={mainRef} style={fadeStyle} className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {reusing
                ? workspaceCreationAllowed
                  ? t(($) => $.step_workspace.eyebrow_resume)
                  : t(($) => $.step_workspace.creation_disabled_eyebrow_resume)
                : workspaceCreationAllowed
                  ? t(($) => $.step_workspace.eyebrow_first)
                  : t(($) => $.step_workspace.creation_disabled_eyebrow)}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {reusing
                ? workspaceCreationAllowed
                  ? t(($) => $.step_workspace.headline_resume, { name: reusing.name })
                  : t(($) => $.step_workspace.creation_disabled_headline_resume, {
                      name: reusing.name,
                    })
                : workspaceCreationAllowed
                  ? t(($) => $.step_workspace.headline_first)
                  : t(($) => $.step_workspace.creation_disabled_headline)}
            </h1>
            <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
              {reusing
                ? workspaceCreationAllowed
                  ? t(($) => $.step_workspace.lede_resume)
                  : t(($) => $.step_workspace.creation_disabled_lede_resume)
                : workspaceCreationAllowed
                  ? t(($) => $.step_workspace.lede_first)
                  : t(($) => $.step_workspace.creation_disabled_lede)}
            </p>

            <div className="mt-10">
              {reusing ? (
                <div className="flex flex-col gap-3">
                  <ExistingWorkspaceCard
                    workspace={reusing}
                    selected={mode === 'existing'}
                    onSelect={pickExisting}
                  />
                  {/* Hide the create-new card entirely when the self-host
                      gate (DISABLE_WORKSPACE_CREATION) is on (#3433) — the
                      backend would 403 the POST and the user would be stuck
                      with a useless form. */}
                  {!workspaceCreationDisabled && (
                    <CreateNewWorkspaceCard selected={mode === 'create'} onSelect={pickCreate}>
                      {createFields}
                    </CreateNewWorkspaceCard>
                  )}
                </div>
              ) : workspaceCreationDisabled ? (
                <CreationDisabledNotice onLogout={logout} />
              ) : (
                createFields
              )}
            </div>

            {!(workspaceCreationDisabled && !reusing) && (
              <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
                <span aria-live="polite" className="mr-auto text-xs text-muted-foreground">
                  {hint}
                </span>
                <Button size="lg" disabled={continueDisabled} onClick={onContinue}>
                  {continueLabel}
                  <ArrowRight className="h-4 w-4" />
                </Button>
              </div>
            )}
          </div>
        </main>
      </div>

      {/* Right — side panel.
          Swap sides based on what the user is currently picking:
          switching to "create" in the resume path swaps the preview
          from "your existing workspace + what's next" to the generic
          "what lives inside / things you'll do here" so the preview
          stays honest to the user's current choice. */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          {reusing && mode !== 'create' ? (
            <ExistingWorkspaceSide workspace={reusing} />
          ) : (
            <CreateWorkspaceSide />
          )}
        </div>
      </aside>
    </div>
  );
}

function CreationDisabledNotice({ onLogout }: { onLogout: () => void }) {
  const { t } = useT('onboarding');
  return (
    <div className="flex flex-col gap-3">
      <Button variant="outline" size="lg" onClick={onLogout}>
        {t(($) => $.step_workspace.creation_disabled_logout)}
      </Button>
    </div>
  );
}

function ExistingWorkspaceCard({
  workspace,
  selected,
  onSelect,
}: {
  workspace: Workspace;
  selected: boolean;
  onSelect: () => void;
}) {
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        'flex w-full items-center gap-4 rounded-lg border bg-card px-5 py-4 text-left transition-all',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20 hover:bg-accent/30',
      )}
    >
      <WorkspaceAvatar name={workspace.name} avatarUrl={workspace.avatar_url} size="lg" />
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="truncate text-[14.5px] font-medium text-foreground">{workspace.name}</div>
        <div className="truncate font-mono text-xs text-muted-foreground">
          {`${urlHost}/${workspace.slug}`}
        </div>
      </div>
      <RadioMark selected={selected} />
    </button>
  );
}

function CreateNewWorkspaceCard({
  selected,
  onSelect,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  children: ReactNode;
}) {
  const { t } = useT('onboarding');
  return (
    <div
      className={cn(
        'overflow-hidden rounded-lg border bg-card transition-all',
        selected
          ? 'border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]'
          : 'hover:border-foreground/20',
      )}
    >
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        aria-expanded={selected}
        onClick={onSelect}
        className="flex w-full items-center gap-4 px-5 py-4 text-left"
      >
        <div
          aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
        >
          <Plus className="h-4 w-4" />
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="truncate text-[14.5px] font-medium text-foreground">
            {t(($) => $.step_workspace.create_new_title)}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {t(($) => $.step_workspace.create_new_subtitle)}
          </div>
        </div>
        <RadioMark selected={selected} />
      </button>
      {selected && <div className="border-t px-5 py-5">{children}</div>}
    </div>
  );
}

function CreateWorkspaceSide() {
  const { t } = useT('onboarding');
  return (
    <div className="flex flex-col gap-6">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_create_eyebrow)}
      </div>

      <WorkspacePreviewCard
        name={t(($) => $.step_workspace.side_preview_name)}
        slug={t(($) => $.step_workspace.side_preview_slug)}
      />

      <div className="mt-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_things_eyebrow)}
      </div>
      <div className="flex flex-col gap-3.5">
        <PerkRow>{t(($) => $.step_workspace.perk_assign)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_chat)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_invite)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.perk_switch)}</PerkRow>
      </div>
    </div>
  );
}

function ExistingWorkspaceSide({ workspace }: { workspace: Workspace }) {
  const { t } = useT('onboarding');
  return (
    <div className="flex flex-col gap-6">
      <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_existing_eyebrow)}
      </div>

      <WorkspacePreviewCard name={workspace.name} slug={workspace.slug} />

      <div className="mt-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
        {t(($) => $.step_workspace.side_next_eyebrow)}
      </div>
      <div className="flex flex-col gap-3.5">
        <PerkRow>{t(($) => $.step_workspace.next_runtime)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.next_agent)}</PerkRow>
        <PerkRow>{t(($) => $.step_workspace.next_starter)}</PerkRow>
      </div>
    </div>
  );
}

function WorkspacePreviewCard({ name, slug }: { name: string; slug: string }) {
  const { t } = useT('onboarding');
  const urlHost = workspaceUrlHost(useConfigStore((s) => s.daemonAppUrl));
  return (
    <div className="overflow-hidden rounded-xl border bg-card shadow-xs">
      <div className="flex items-center gap-3 border-b px-4 py-3.5">
        <WorkspaceAvatar name={name} size="md" />
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="truncate text-[14px] font-medium text-foreground">{name}</div>
          <div className="truncate font-mono text-[11.5px] text-muted-foreground">
            {`${urlHost}/${slug}`}
          </div>
        </div>
        <Lock aria-hidden className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
      </div>
      <div className="flex flex-col">
        <EntityRow
          icon={<Inbox className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.inbox_label)}
          meta={t(($) => $.step_workspace.preview.inbox_meta)}
        />
        <EntityRow
          icon={<ListTodo className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.issues_label)}
          meta={t(($) => $.step_workspace.preview.issues_meta)}
        />
        <EntityRow
          icon={<Bot className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.agents_label)}
          meta={t(($) => $.step_workspace.preview.agents_meta)}
        />
        <EntityRow
          icon={<FolderKanban className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.projects_label)}
          meta={t(($) => $.step_workspace.preview.projects_meta)}
        />
        <EntityRow
          icon={<Zap className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.autopilot_label)}
          meta={t(($) => $.step_workspace.preview.autopilot_meta)}
        />
        <EntityRow
          dim
          icon={<Monitor className="h-4 w-4" />}
          label={t(($) => $.step_workspace.preview.settings_label)}
          meta={t(($) => $.step_workspace.preview.settings_meta)}
        />
      </div>
    </div>
  );
}

function EntityRow({
  icon,
  label,
  meta,
  dim,
}: {
  icon: ReactNode;
  label: string;
  meta: string;
  dim?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 [&:not(:last-child)]:border-b">
      <span
        aria-hidden
        className={cn('shrink-0', dim ? 'text-muted-foreground/60' : 'text-muted-foreground')}
      >
        {icon}
      </span>
      <span
        className={cn('flex-1 text-[13.5px]', dim ? 'text-muted-foreground' : 'text-foreground')}
      >
        {label}
      </span>
      <span
        className={cn(
          'font-mono text-[11.5px]',
          dim ? 'text-muted-foreground/70' : 'text-muted-foreground',
        )}
      >
        {meta}
      </span>
    </div>
  );
}

function PerkRow({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[18px_1fr] items-start gap-3">
      <span aria-hidden className="mt-[11px] h-px w-3 shrink-0 bg-muted-foreground/40" />
      <div className="text-[13.5px] leading-[1.55] text-foreground/85">{children}</div>
    </div>
  );
}

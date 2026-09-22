'use client';

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowLeftRight,
  CalendarDays,
  Check,
  ChevronRight,
  FolderKanban,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Settings2,
  X as XIcon,
} from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { toast } from 'sonner';
import { DialogTitle } from '@goosar/ui/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import { Button } from '@goosar/ui/components/ui/button';
import { Switch } from '@goosar/ui/components/ui/switch';
import { api, ApiError } from '@goosar/core/api';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useCurrentWorkspace, useWorkspacePaths } from '@goosar/core/paths';
import { useNavigation } from '../navigation';
import { agentListOptions, squadListOptions } from '@goosar/core/workspace/queries';
import { projectListOptions } from '@goosar/core/projects/queries';
import {
  useQuickCreateStore,
  type QuickCreateActorType,
} from '@goosar/core/issues/stores/quick-create-store';
import {
  useIssueCreateSettingsStore,
  type QuickCreateField,
} from '@goosar/core/issues/stores/issue-create-settings-store';
import { useIssueDraftStore, type IssueCreateDraft } from '@goosar/core/issues/stores/draft-store';
import { useCreateModeStore } from '@goosar/core/issues/stores/create-mode-store';
import {
  runtimeListOptions,
  checkQuickCreateCliVersion,
  checkQuickCreateFieldsCliVersion,
  readRuntimeCliVersion,
} from '@goosar/core/runtimes';
import { useShortcut } from '@goosar/core/shortcuts';
import { ShortcutKeycaps } from '../common/shortcut-keycaps';
import {
  contentReferencesAttachment,
  type Agent,
  type IssuePriority,
  type Squad,
} from '@goosar/core/types';
import { ActorAvatar } from '../common/actor-avatar';
import { PillButton } from '../common/pill-button';
import { ProjectPicker } from '../projects/components/project-picker';
import { DueDatePicker, PriorityIcon, PriorityPicker } from '../issues/components';
import { canAssignAgent } from '../issues/components/pickers/assignee-picker';
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from '../issues/components/pickers/property-picker';
import { useAuthStore } from '@goosar/core/auth';
import { memberListOptions } from '@goosar/core/workspace/queries';
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useUploadGate,
  useComposerSubmit,
} from '../editor';
import { useIssueCreateUploads } from './use-issue-create-uploads';
import { FileUploadButton } from '@goosar/ui/components/common/file-upload-button';
import { useT } from '../i18n';
import { matchesPinyin } from '../editor/extensions/pinyin-match';

type ActorSelection = { type: 'agent'; id: string } | { type: 'squad'; id: string };

export function AgentCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
}: {
  onClose: () => void;
  onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
  isExpanded: boolean;
  setIsExpanded: (v: boolean) => void;
}) {
  const { t } = useT('modals');
  const sendShortcut = useShortcut('send');
  const workspaceName = useCurrentWorkspace()?.name;
  const workspacePaths = useWorkspacePaths();
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const { data: projects = [], isSuccess: projectsLoaded } = useQuery(projectListOptions(wsId));

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  const visibleAgents = useMemo(
    () => agents.filter((a) => !a.archived_at && canAssignAgent(a, userId, memberRole)),
    [agents, userId, memberRole],
  );
  const visibleAgentIds = useMemo(() => new Set(visibleAgents.map((a) => a.id)), [visibleAgents]);
  const visibleSquads = useMemo(
    () => squads.filter((s) => !s.archived_at && visibleAgentIds.has(s.leader_id)),
    [squads, visibleAgentIds],
  );

  const lastActorType = useQuickCreateStore((s) => s.lastActorType);
  const lastActorId = useQuickCreateStore((s) => s.lastActorId);
  const setLastActor = useQuickCreateStore((s) => s.setLastActor);
  const lastProjectId = useQuickCreateStore((s) => s.lastProjectId);
  const setLastProjectId = useQuickCreateStore((s) => s.setLastProjectId);
  const visibleFields = useIssueCreateSettingsStore((s) => s.quickCreateFields);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const draft = useIssueDraftStore((s) => s.draft);
  const setShared = useIssueDraftStore((s) => s.setShared);
  const setManual = useIssueDraftStore((s) => s.setManual);
  const setAgent = useIssueDraftStore((s) => s.setAgent);
  const setActiveMode = useIssueDraftStore((s) => s.setActiveMode);
  const clearDraft = useIssueDraftStore((s) => s.clearDraft);

  const resolveActor = useCallback(
    (
      type: QuickCreateActorType | 'agent' | 'squad' | null | undefined,
      id: string | null | undefined,
    ): ActorSelection | null => {
      if (!type || !id) return null;
      if (type === 'squad' && visibleSquads.some((s) => s.id === id)) {
        return { type: 'squad', id };
      }
      if (type === 'agent' && visibleAgentIds.has(id)) {
        return { type: 'agent', id };
      }
      return null;
    },
    [visibleSquads, visibleAgentIds],
  );

  const seedActor = useCallback((): ActorSelection | null => {
    const dataAgent = data?.agent_id as string | undefined;
    const dataSquad = data?.squad_id as string | undefined;
    return (
      resolveActor('agent', dataAgent) ||
      resolveActor('squad', dataSquad) ||
      resolveActor(draft.agent.actorType, draft.agent.actorId) ||
      resolveActor(lastActorType, lastActorId) ||
      (visibleAgents[0] ? ({ type: 'agent', id: visibleAgents[0].id } as const) : null)
    );
  }, [
    resolveActor,
    data?.agent_id,
    data?.squad_id,
    draft.agent.actorType,
    draft.agent.actorId,
    lastActorType,
    lastActorId,
    visibleAgents,
  ]);

  const [actor, setActor] = useState<ActorSelection | null>(() => seedActor());

  useEffect(() => {
    if (actor && resolveActor(actor.type, actor.id)) return;
    setActor(seedActor());
  }, [actor, resolveActor, seedActor]);

  const selectedAgent = useMemo<Agent | undefined>(() => {
    if (!actor) return undefined;
    if (actor.type === 'agent') return visibleAgents.find((a) => a.id === actor.id);
    const squad = visibleSquads.find((s) => s.id === actor.id);
    if (!squad) return undefined;
    return visibleAgents.find((a) => a.id === squad.leader_id);
  }, [actor, visibleAgents, visibleSquads]);

  const selectedSquad = useMemo<Squad | undefined>(() => {
    if (actor?.type !== 'squad') return undefined;
    return visibleSquads.find((s) => s.id === actor.id);
  }, [actor, visibleSquads]);

  const [projectId, setProjectId] = useState<string | null>(() => {
    const seed =
      (data?.project_id as string | undefined) ?? draft.shared.projectId ?? lastProjectId;
    return seed ?? null;
  });
  const [priority, setPriority] = useState<IssuePriority>(
    (data?.priority as IssuePriority | undefined) ?? draft.shared.priority,
  );
  const [dueDate, setDueDate] = useState<string | null>(
    (data?.due_date as string | undefined) ?? draft.shared.dueDate,
  );
  const [fieldPickerOpen, setFieldPickerOpen] = useState<QuickCreateField | null>(null);

  const parentIssueId = (data?.parent_issue_id as string | undefined) ?? undefined;
  const parentIssueIdentifier = (data?.parent_issue_identifier as string | undefined) ?? undefined;

  useEffect(() => {
    if (!projectsLoaded || projectId === null) return;
    if (projects.some((p) => p.id === projectId)) return;
    setProjectId(null);
    if (draft.shared.projectId === projectId) {
      setShared({ projectId: undefined });
    }
    if (lastProjectId === projectId) setLastProjectId(null);
  }, [
    projectsLoaded,
    projects,
    projectId,
    draft.shared.projectId,
    lastProjectId,
    setShared,
    setLastProjectId,
  ]);

  useEffect(() => {
    setActiveMode('agent');
  }, [setActiveMode]);

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const selectedRuntime = useMemo(
    () =>
      selectedAgent?.runtime_id
        ? runtimes.find((r) => r.id === selectedAgent.runtime_id)
        : undefined,
    [runtimes, selectedAgent?.runtime_id],
  );
  const runtimeCliVersion = readRuntimeCliVersion(selectedRuntime?.metadata);
  const baseVersionCheck = useMemo(
    () => checkQuickCreateCliVersion(runtimeCliVersion),
    [runtimeCliVersion],
  );
  const fieldVersionCheck = useMemo(
    () => checkQuickCreateFieldsCliVersion(runtimeCliVersion),
    [runtimeCliVersion],
  );
  const usesExplicitFields = priority !== 'none' || dueDate !== null;
  const versionCheck = usesExplicitFields ? fieldVersionCheck : baseVersionCheck;
  const versionBlocked =
    baseVersionCheck.state !== 'ok' || (usesExplicitFields && fieldVersionCheck.state !== 'ok');

  const initialPrompt = draft.agent.prompt || (data?.prompt as string) || '';
  const editorRef = useRef<ContentEditorRef>(null);
  const [hasContent, setHasContent] = useState(initialPrompt.trim().length > 0);
  const [justSent, setJustSent] = useState(false);
  const [sentCount, setSentCount] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const uploadGate = useUploadGate(editorRef);
  const {
    attachments: pendingAttachments,
    handleUpload: handleUploadFile,
    gate,
  } = useIssueCreateUploads('agent', uploadGate, editorRef);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  useEffect(() => {
    const id = requestAnimationFrame(() => editorRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, []);

  const mountedRef = useRef(true);
  useLayoutEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedDraftRef = useRef<IssueCreateDraft | null>(null);
  const refocusAfterAcceptRef = useRef(false);

  const composer = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    onSubmit: async (md): Promise<boolean> => {
      if (!actor || versionBlocked) return false;
      const pendingPrompt = editorRef.current?.flushPendingUpdate?.();
      if (pendingPrompt != null) setAgent({ prompt: pendingPrompt });
      submittedDraftRef.current = useIssueDraftStore.getState().draft;
      const activeAttachmentIds = pendingAttachments
        .filter((a) => contentReferencesAttachment(md, a))
        .map((a) => a.id);
      setError(null);
      try {
        await api.quickCreateIssue({
          ...(actor.type === 'agent' ? { agent_id: actor.id } : { squad_id: actor.id }),
          prompt: md,
          project_id: projectId ?? undefined,
          ...(priority !== 'none' ? { priority } : {}),
          ...(dueDate ? { due_date: dueDate } : {}),
          parent_issue_id: parentIssueId,
          ...(activeAttachmentIds.length > 0 ? { attachment_ids: activeAttachmentIds } : {}),
        });
        setLastActor(actor.type, actor.id);
        setLastProjectId(projectId);
        setLastMode('agent');
        toast.success(
          t(($) => $.create_issue.agent.toast_sent),
          {
            duration: 4000,
          },
        );
        return true;
      } catch (e) {
        if (e instanceof ApiError && e.body && typeof e.body === 'object') {
          const body = e.body as {
            code?: string;
            reason?: string;
            current_version?: string;
            min_version?: string;
          };
          if (body.code === 'agent_unavailable') {
            setError(
              body.reason || t(($) => $.create_issue.agent.error_agent_unavailable_fallback),
            );
            return false;
          }
          if (body.code === 'daemon_version_unsupported') {
            const cur = body.current_version || 'unknown';
            setError(
              t(($) => $.create_issue.agent.error_daemon_version, {
                current: cur,
                min: body.min_version || versionCheck.min,
              }),
            );
            return false;
          }
        }
        setError(
          e instanceof Error && e.message
            ? e.message
            : t(($) => $.create_issue.agent.error_unknown),
        );
        return false;
      }
    },
    afterAccepted: () => (refocusAfterAcceptRef.current ? 'refocus' : 'none'),
    onAccepted: () => {
      refocusAfterAcceptRef.current = false;
      const latePrompt = editorRef.current?.flushPendingUpdate?.();
      if (latePrompt != null) setAgent({ prompt: latePrompt });
      const untouched = useIssueDraftStore.getState().draft === submittedDraftRef.current;
      if (untouched) clearDraft();
      if (!mountedRef.current || !untouched) return;
      if (keepOpen) {
        editorRef.current?.clearContent();
        setHasContent(false);
        setSentCount((c) => c + 1);
        setJustSent(true);
        setTimeout(() => setJustSent(false), 1500);
        refocusAfterAcceptRef.current = true;
      } else {
        onClose();
      }
    },
  });
  const submit = () => {
    void composer.submit();
  };
  const submitting = composer.submitting;

  const switchToManual = () => {
    if (gate.isBlocked()) return;
    setShared({ projectId: projectId ?? undefined, priority, dueDate });
    if (!draft.manual.description.trim()) {
      const md = editorRef.current?.getMarkdown() ?? '';
      if (md) setManual({ description: md });
    }
    if (!draft.manual.assigneeId && actor) {
      setManual({ assigneeType: actor.type, assigneeId: actor.id });
    }
    setLastMode('manual');
    setActiveMode('manual');
    const carry: Record<string, unknown> = {};
    if (parentIssueId) carry.parent_issue_id = parentIssueId;
    if (parentIssueIdentifier) carry.parent_issue_identifier = parentIssueIdentifier;
    onSwitchMode?.(Object.keys(carry).length > 0 ? carry : null);
  };

  const openFieldSettings = () => {
    setAgent({ prompt: editorRef.current?.getMarkdown() ?? '' });
    onClose();
    navigation.push(`${workspacePaths.settings()}?tab=issue`);
  };

  return (
    <>
      <DialogTitle className="sr-only">{t(($) => $.create_issue.sr_agent)}</DialogTitle>

      {/* Header */}
      <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
        <div className="flex items-center gap-1.5 text-xs">
          <span className="text-muted-foreground">{workspaceName}</span>
          <ChevronRight className="size-3 text-muted-foreground/50" />
          <span className="font-medium">{t(($) => $.create_issue.agent_breadcrumb)}</span>
        </div>
        {/* Native `title` instead of Base UI Tooltip — Tooltip opens on
              keyboard focus, and the dialog's focus trap briefly lands focus
              on the first focusable element on mount, causing the tooltip to
              auto-pop every open. Same workaround applies to expand. */}
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => setIsExpanded(!isExpanded)}
            title={
              isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)
            }
            aria-label={
              isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)
            }
            className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
          >
            {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
          </button>
          <button
            type="button"
            onClick={onClose}
            title={t(($) => $.common.close)}
            aria-label={t(($) => $.common.close)}
            className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
          >
            <XIcon className="size-4" />
          </button>
        </div>
      </div>

      {/* Actor picker — agents and squads in one searchable list. Squads
            route to their leader agent on the backend; the leader runs the
            quick-create flow with the squad's Operating Protocol layered
            on top, so a squad pick is "ask this squad to file the issue". */}
      <div className="px-5 pt-1 pb-2 shrink-0">
        <ActorPicker
          actor={actor}
          visibleAgents={visibleAgents}
          visibleSquads={visibleSquads}
          selectedAgent={selectedAgent}
          selectedSquad={selectedSquad}
          onPick={(next) => {
            setActor(next);
            setAgent({ actorType: next.type, actorId: next.id });
            setError(null);
          }}
          t={t}
        />
      </div>

      {selectedAgent && versionBlocked && (
        <div className="mx-5 mb-2 shrink-0 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
          {versionCheck.state === 'missing'
            ? t(($) => $.create_issue.agent.version_missing, { min: versionCheck.min })
            : t(($) => $.create_issue.agent.version_below, {
                current: versionCheck.current,
                min: versionCheck.min,
              })}
        </div>
      )}

      {/* Prompt — same rich editor Advanced uses, so paste/drop images,
            mentions, and formatting all work. The dropZone wrapper enables
            drag-and-drop file uploads alongside paste. */}
      {/* `flex-1 min-h-0 overflow-y-auto` so the editor area absorbs the
            remaining vertical space inside the (now max-bounded) DialogContent
            and scrolls internally. Without it, pasting an image expanded the
            editor unbounded and pushed the modal past the viewport. */}
      <div
        {...dropZoneProps}
        className="relative px-5 pb-3 flex flex-1 min-h-[140px] overflow-y-auto"
      >
        <ContentEditor
          ref={editorRef}
          defaultValue={initialPrompt}
          placeholder={t(($) => $.create_issue.agent.prompt_placeholder)}
          onUpdate={(md) => {
            setHasContent(md.trim().length > 0);
            setAgent({ prompt: md });
          }}
          onUploadFile={handleUploadFile}
          onUploadingChange={uploadGate.onUploadingChange}
          attachments={pendingAttachments}
          onSubmit={submit}
          debounceMs={150}
        />
        {isDragOver && <FileDropOverlay />}
      </div>

      {error && <div className="px-5 pb-2 text-xs text-destructive">{error}</div>}

      {/* Property toolbar — the project is visible by default; priority and
            due date live behind the overflow until exposed in settings or
            given a value. Unfinished picks remain workspace-persistent.
            When the modal was opened from "Add sub issue" on an existing
            issue, a read-only chip on the same row tells the user that the
            new issue will be filed as a sub-issue of that parent — the agent
            handles the wiring silently, but surfacing the relationship
            avoids "where did this end up?" surprise. We deliberately keep
            it non-editable: changing the parent is a `Set parent` action on
            the parent itself, not a knob in the quick-create flow. */}
      <div className="flex items-center gap-1.5 px-4 pb-2 shrink-0 flex-wrap">
        {(visibleFields.includes('project') ||
          projectId !== null ||
          fieldPickerOpen === 'project') && (
          <ProjectPicker
            projectId={projectId}
            onUpdate={(u) => {
              const next = u.project_id ?? null;
              setProjectId(next);
              setShared({ projectId: next ?? undefined });
            }}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'project' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'project' : null)}
          />
        )}
        {(visibleFields.includes('priority') ||
          priority !== 'none' ||
          fieldPickerOpen === 'priority') && (
          <PriorityPicker
            priority={priority}
            onUpdate={(updates) => {
              if (updates.priority) {
                setPriority(updates.priority);
                setShared({ priority: updates.priority });
              }
            }}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'priority' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'priority' : null)}
          />
        )}
        {(visibleFields.includes('due_date') ||
          dueDate !== null ||
          fieldPickerOpen === 'due_date') && (
          <DueDatePicker
            dueDate={dueDate}
            onUpdate={(updates) => {
              const next = updates.due_date ?? null;
              setDueDate(next);
              setShared({ dueDate: next });
            }}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'due_date' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'due_date' : null)}
          />
        )}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <PillButton
                aria-label={t(($) => $.create_issue.agent.more_fields_aria)}
                title={t(($) => $.create_issue.agent.more_fields_aria)}
              >
                <MoreHorizontal className="size-3.5" />
              </PillButton>
            }
          />
          <DropdownMenuContent align="start" className="w-52">
            {!visibleFields.includes('project') && projectId === null && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('project')}>
                <FolderKanban className="size-3.5 text-muted-foreground" />
                {t(($) => $.create_issue.agent.set_project)}
              </DropdownMenuItem>
            )}
            {!visibleFields.includes('priority') && priority === 'none' && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('priority')}>
                <PriorityIcon priority="none" className="size-3.5" />
                {t(($) => $.create_issue.agent.set_priority)}
              </DropdownMenuItem>
            )}
            {!visibleFields.includes('due_date') && dueDate === null && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('due_date')}>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {t(($) => $.create_issue.agent.set_due_date)}
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={openFieldSettings}>
              <Settings2 className="size-3.5 text-muted-foreground" />
              {t(($) => $.create_issue.agent.customize_fields)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {parentIssueId && (
          <span
            data-testid="agent-sub-issue-chip"
            className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
            title={t(($) => $.create_issue.agent.sub_issue_of, {
              identifier: parentIssueIdentifier ?? '',
            })}
          >
            {t(($) => $.create_issue.agent.sub_issue_of, {
              identifier: parentIssueIdentifier ?? '',
            })}
          </span>
        )}
      </div>

      {/* Footer */}
      <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-h-7 items-center gap-2">
          {/* Deliberately NOT disabled while uploading: each file is its
                own queue entry, so queueing a second one is safe and waiting
                for the first to land just to attach the next is busywork. */}
          <FileUploadButton
            size="sm"
            multiple
            onSelect={(file) => editorRef.current?.uploadFile(file)}
          />
          {keepOpen && sentCount > 0 && (
            <span className="text-xs text-emerald-600 dark:text-emerald-400">
              {t(($) => $.create_issue.agent.sent_count, { count: sentCount })}
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            onClick={switchToManual}
            disabled={gate.uploading}
            aria-disabled={gate.uploading || undefined}
            aria-busy={gate.uploading || undefined}
            title={t(($) => $.create_issue.switch_to_manual_tooltip)}
            className="flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ArrowLeftRight className="size-3.5" />
            {t(($) => $.create_issue.switch_to_manual)}
          </button>
          <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
            <Switch size="sm" checked={keepOpen} onCheckedChange={setKeepOpen} />
            {t(($) => $.create_issue.create_another)}
          </label>
          <Button
            size="sm"
            onClick={submit}
            disabled={!hasContent || !actor || submitting || versionBlocked || gate.uploading}
            aria-disabled={gate.uploading || undefined}
            aria-busy={gate.uploading || submitting || undefined}
            title={
              versionBlocked
                ? t(($) => $.create_issue.agent.version_blocked_tooltip, { min: versionCheck.min })
                : undefined
            }
            className={justSent ? 'min-w-28 !bg-emerald-600 !text-white' : 'min-w-28'}
          >
            {submitting ? (
              t(($) => $.create_issue.agent.sending)
            ) : gate.uploading ? (
              t(($) => $.create_issue.agent.uploading)
            ) : justSent ? (
              <span className="flex items-center gap-1">
                <Check className="size-3.5" />
                {t(($) => $.create_issue.agent.sent_label)}
              </span>
            ) : (
              <>
                {t(($) => $.create_issue.agent.submit)}
                {sendShortcut ? (
                  <ShortcutKeycaps
                    shortcut={sendShortcut}
                    decorative
                    className="ml-1"
                    keyClassName="border-background/30 bg-background/15 text-primary-foreground shadow-none"
                  />
                ) : null}
              </>
            )}
          </Button>
        </div>
      </div>
    </>
  );
}

function ActorPicker({
  actor,
  visibleAgents,
  visibleSquads,
  selectedAgent,
  selectedSquad,
  onPick,
  t,
}: {
  actor: ActorSelection | null;
  visibleAgents: Agent[];
  visibleSquads: Squad[];
  selectedAgent: Agent | undefined;
  selectedSquad: Squad | undefined;
  onPick: (next: ActorSelection) => void;
  t: ReturnType<typeof useT<'modals'>>['t'];
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const query = filter.trim().toLowerCase();

  const filteredAgents = useMemo(
    () =>
      visibleAgents.filter(
        (a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query),
      ),
    [visibleAgents, query],
  );
  const filteredSquads = useMemo(
    () =>
      visibleSquads.filter(
        (s) => s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query),
      ),
    [visibleSquads, query],
  );

  const displayLabel = selectedSquad?.name ?? selectedAgent?.name;
  const displayActor: ActorSelection | null = selectedSquad
    ? { type: 'squad', id: selectedSquad.id }
    : selectedAgent
      ? { type: 'agent', id: selectedAgent.id }
      : null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter('');
      }}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.create_issue.agent.search_placeholder)}
      onSearchChange={setFilter}
      trigger={
        <span className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors">
          <span>{t(($) => $.create_issue.agent.created_by)}</span>
          {displayActor && displayLabel ? (
            <span className="flex items-center gap-1.5 text-foreground">
              <ActorAvatar actorType={displayActor.type} actorId={displayActor.id} size="sm" />
              {displayLabel}
            </span>
          ) : (
            <span>{t(($) => $.create_issue.agent.pick_an_agent)}</span>
          )}
        </span>
      }
    >
      {filteredAgents.length === 0 && filteredSquads.length === 0 ? (
        query ? (
          <PickerEmpty />
        ) : (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.create_issue.agent.no_agents)}
          </div>
        )
      ) : (
        <>
          {filteredAgents.length > 0 && (
            <PickerSection label={t(($) => $.create_issue.agent.agents_group)}>
              {filteredAgents.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={actor?.type === 'agent' && actor.id === a.id}
                  onClick={() => {
                    onPick({ type: 'agent', id: a.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size="sm" />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredSquads.length > 0 && (
            <PickerSection label={t(($) => $.create_issue.agent.squads_group)}>
              {filteredSquads.map((s) => (
                <PickerItem
                  key={s.id}
                  selected={actor?.type === 'squad' && actor.id === s.id}
                  onClick={() => {
                    onPick({ type: 'squad', id: s.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                  <span className="truncate">{s.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}

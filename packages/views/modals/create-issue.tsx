'use client';

import { useState, useRef, useEffect, useLayoutEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigation } from '../navigation';
import {
  AlertTriangle,
  ArrowDown,
  ArrowLeftRight,
  ArrowUp,
  CalendarClock,
  CalendarDays,
  Check,
  ChevronRight,
  CircleUser,
  FolderKanban,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Settings2,
  Shapes,
  Tag,
  X as XIcon,
} from 'lucide-react';
import { cn } from '@goosar/ui/lib/utils';
import { toast } from 'sonner';
import type {
  Issue,
  IssueStatus,
  IssuePriority,
  IssueAssigneeType,
  IssuePropertyValue,
} from '@goosar/core/types';
import { contentReferencesAttachment } from '@goosar/core/types';
import { DialogContent, DialogTitle } from '@goosar/ui/components/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@goosar/ui/components/ui/dropdown-menu';
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TooltipProvider,
} from '@goosar/ui/components/ui/tooltip';
import { Button } from '@goosar/ui/components/ui/button';
import { Switch } from '@goosar/ui/components/ui/switch';
import {
  ContentEditor,
  type ContentEditorRef,
  TitleEditor,
  type TitleEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useUploadGate,
  useComposerSubmit,
} from '../editor';
import { useIssueCreateUploads } from './use-issue-create-uploads';
import { useShortcut } from '@goosar/core/shortcuts';
import { ShortcutKeycaps } from '../common/shortcut-keycaps';
import {
  StatusIcon,
  StatusPicker,
  PriorityIcon,
  PriorityPicker,
  StagePicker,
  AssigneePicker,
  StartDatePicker,
  DueDatePicker,
  LabelPicker,
} from '../issues/components';
import { maxSiblingStage } from '../issues/components/pickers/stage-picker';
import { ProjectPicker } from '../projects/components/project-picker';
import { useIssueTriggerPreview } from '../issues/hooks/use-issue-trigger-preview';
import { useActorName } from '@goosar/core/workspace/hooks';
import { useCurrentWorkspace, useWorkspacePaths } from '@goosar/core/paths';
import { useWorkspaceId } from '@goosar/core/hooks';
import { useIssueDraftStore, type IssueCreateDraft } from '@goosar/core/issues/stores/draft-store';
import { useCreateModeStore } from '@goosar/core/issues/stores/create-mode-store';
import { useQuickCreateStore } from '@goosar/core/issues/stores/quick-create-store';
import {
  useIssueCreateSettingsStore,
  type ManualCreateField,
} from '@goosar/core/issues/stores/issue-create-settings-store';
import { issueDetailOptions, childIssuesOptions } from '@goosar/core/issues/queries';
import { useCreateIssue, useUpdateIssue } from '@goosar/core/issues/mutations';
import { useAttachLabelToIssue } from '@goosar/core/labels';
import { propertyListOptions, useSetIssueProperty } from '@goosar/core/properties';
import {
  ApiError,
  DuplicateIssueErrorBodySchema,
  type DuplicateIssueErrorBody,
  parseWithFallback,
} from '@goosar/core/api';
import { FileUploadButton } from '@goosar/ui/components/common/file-upload-button';
import { PillButton } from '../common/pill-button';
import { ActorAvatar } from '../common/actor-avatar';
import { PropertyIcon } from '../common/property-icon';
import {
  CustomPropertyValueDisplay,
  CustomPropertyValueInput,
} from '../issues/components/pickers/custom-property-picker';
import { IssuePickerModal } from './issue-picker-modal';
import { useT } from '../i18n';

function CreateRunHint({
  assigneeType,
  assigneeId,
  status,
}: {
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  status: IssueStatus;
}) {
  const { t } = useT('modals');
  const { getActorName } = useActorName();
  const isAgentLike = assigneeType === 'agent' || assigneeType === 'squad';
  const preview = useIssueTriggerPreview({
    isCreate: true,
    assigneeType: assigneeType ?? null,
    assigneeId: assigneeId ?? null,
    status,
    enabled: isAgentLike && !!assigneeId,
  });

  const ready = isAgentLike && !!assigneeId && !preview.isLoading;
  const willStart = preview.totalCount > 0;
  const isSquad = assigneeType === 'squad';
  const triggerAgentId = preview.triggers[0]?.agent_id ?? assigneeId;

  let avatarType: string;
  let avatarId: string | undefined;
  let text: string;
  if (!willStart) {
    avatarType = assigneeType ?? 'agent';
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_parked);
  } else if (isSquad) {
    avatarType = 'squad';
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_will_start_squad, {
      name: getActorName('squad', assigneeId ?? ''),
    });
  } else {
    avatarType = 'agent';
    avatarId = triggerAgentId;
    text = t(($) => $.run_confirm.create_will_start, {
      name: getActorName('agent', triggerAgentId ?? assigneeId ?? ''),
    });
  }

  return (
    <div
      className={cn(
        'grid shrink-0 transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none',
        ready ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]',
      )}
      aria-hidden={!ready}
    >
      <div className="overflow-hidden">
        <div
          aria-live="polite"
          className="flex items-center gap-1.5 px-4 pb-1 pt-0.5 text-[0.6875rem] text-muted-foreground"
        >
          {avatarId && (
            <ActorAvatar actorType={avatarType} actorId={avatarId} size="sm" profileLink={false} />
          )}
          <span className="truncate">{text}</span>
        </div>
      </div>
    </div>
  );
}

export function ManualCreatePanel({
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
  const { t: tEditor } = useT('editor');
  const router = useNavigation();
  const p = useWorkspacePaths();
  const workspaceName = useCurrentWorkspace()?.name;

  const draft = useIssueDraftStore((s) => s.draft);
  const setManual = useIssueDraftStore((s) => s.setManual);
  const setShared = useIssueDraftStore((s) => s.setShared);
  const setAgent = useIssueDraftStore((s) => s.setAgent);
  const setActiveMode = useIssueDraftStore((s) => s.setActiveMode);
  const clearDraft = useIssueDraftStore((s) => s.clearDraft);
  const setLastAssignee = useIssueDraftStore((s) => s.setLastAssignee);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const manualFields = useIssueCreateSettingsStore((s) => s.manualCreateFields);

  const sendShortcut = useShortcut('send');
  const [title, setTitle] = useState(draft.manual.title);
  const [formResetKey, setFormResetKey] = useState(0);
  const titleEditorRef = useRef<TitleEditorRef>(null);
  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => descEditorRef.current?.uploadFile(f)),
  });
  const [status, setStatus] = useState<IssueStatus>(
    (data?.status as IssueStatus) || draft.manual.status,
  );
  const [priority, setPriority] = useState<IssuePriority>(
    (data?.priority as IssuePriority | undefined) ?? draft.shared.priority,
  );
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(() => {
    if (data && 'assignee_type' in data) {
      return (data.assignee_type as IssueAssigneeType | null) ?? undefined;
    }
    return draft.manual.assigneeType;
  });
  const [assigneeId, setAssigneeId] = useState<string | undefined>(() => {
    if (data && 'assignee_id' in data) {
      return (data.assignee_id as string | null) ?? undefined;
    }
    return draft.manual.assigneeId;
  });
  const [startDate, setStartDate] = useState<string | null>(draft.manual.startDate);
  const [dueDate, setDueDate] = useState<string | null>(
    (data?.due_date as string | undefined) ?? draft.shared.dueDate,
  );
  const [labelIds, setLabelIds] = useState<string[]>(draft.manual.labelIds);
  const [propertyValues, setPropertyValues] = useState(draft.manual.propertyValues ?? {});
  const [customPropertyPickerId, setCustomPropertyPickerId] = useState<string | null>(null);
  const [projectId, setProjectId] = useState<string | undefined>(() => {
    if (data && 'project_id' in data) {
      return (data.project_id as string | null) ?? undefined;
    }
    return draft.shared.projectId;
  });
  const [parentIssueId, setParentIssueId] = useState<string | undefined>(
    (data?.parent_issue_id as string) || undefined,
  );
  const [stage, setStage] = useState<number | null>(
    typeof data?.stage === 'number' ? (data.stage as number) : null,
  );
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  const [fieldPickerOpen, setFieldPickerOpen] = useState<Exclude<
    ManualCreateField,
    'due_date' | 'start_date'
  > | null>(null);
  const [startDatePickerOpen, setStartDatePickerOpen] = useState(false);
  const [dueDatePickerOpen, setDueDatePickerOpen] = useState(false);
  const [childIssues, setChildIssues] = useState<Issue[]>([]);
  const [childPickerOpen, setChildPickerOpen] = useState(false);
  const wsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const { data: parentIssue } = useQuery({
    ...issueDetailOptions(wsId, parentIssueId ?? ''),
    enabled: !!parentIssueId,
  });
  const { data: parentChildren = [] } = useQuery({
    ...childIssuesOptions(wsId, parentIssueId ?? ''),
    enabled: !!parentIssueId,
  });

  useEffect(() => {
    setActiveMode('manual');
  }, [setActiveMode]);

  useEffect(() => {
    const { draft: current } = useIssueDraftStore.getState();
    const uploads = current.shared.attachments ?? [];
    const kept = uploads.filter(
      (u) =>
        u.status !== 'uploaded' ||
        contentReferencesAttachment(current.manual.description, u.attachment) ||
        contentReferencesAttachment(current.agent.prompt, u.attachment),
    );
    if (kept.length !== uploads.length) setShared({ attachments: kept });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const uploadGate = useUploadGate(descEditorRef);
  const {
    attachments: draftAttachments,
    handleUpload,
    gate,
  } = useIssueCreateUploads('manual', uploadGate, descEditorRef);

  const updateTitle = (v: string) => {
    setTitle(v);
    setManual({ title: v });
  };
  const updateStatus = (v: IssueStatus) => {
    setStatus(v);
    setManual({ status: v });
  };
  const updatePriority = (v: IssuePriority) => {
    setPriority(v);
    setShared({ priority: v });
  };
  const updateAssignee = (type?: IssueAssigneeType, id?: string) => {
    setAssigneeType(type);
    setAssigneeId(id);
    setManual({ assigneeType: type, assigneeId: id });
  };
  const updateProject = (id?: string) => {
    setProjectId(id);
    setShared({ projectId: id });
  };
  const updateStartDate = (v: string | null) => {
    setStartDate(v);
    setManual({ startDate: v });
  };
  const updateDueDate = (v: string | null) => {
    setDueDate(v);
    setShared({ dueDate: v });
  };
  const updateLabelIds = (ids: string[]) => {
    setLabelIds(ids);
    setManual({ labelIds: ids });
  };
  const updatePropertyValue = (propertyId: string, value: IssuePropertyValue | undefined) => {
    const next = { ...propertyValues };
    if (value === undefined) delete next[propertyId];
    else next[propertyId] = value;
    setPropertyValues(next);
    setManual({ propertyValues: next });
  };

  const showField = {
    status: manualFields.includes('status') || status !== 'todo' || fieldPickerOpen === 'status',
    priority:
      manualFields.includes('priority') || priority !== 'none' || fieldPickerOpen === 'priority',
    assignee:
      manualFields.includes('assignee') || assigneeId != null || fieldPickerOpen === 'assignee',
    labels: manualFields.includes('labels') || labelIds.length > 0 || fieldPickerOpen === 'labels',
    project: manualFields.includes('project') || projectId != null || fieldPickerOpen === 'project',
    due_date: manualFields.includes('due_date') || dueDate !== null || dueDatePickerOpen,
    start_date: manualFields.includes('start_date') || startDate !== null || startDatePickerOpen,
  };

  const openFieldSettings = () => {
    onClose();
    router.push(`${p.settings()}?tab=issue`);
  };

  const createIssueMutation = useCreateIssue();
  const updateIssueMutation = useUpdateIssue();
  const attachLabelMutation = useAttachLabelToIssue();
  const setIssuePropertyMutation = useSetIssueProperty();
  const resetForNextIssue = () => {
    setTitle('');
    setStatus('todo');
    setPriority('none');
    setStartDate(null);
    setDueDate(null);
    setLabelIds([]);
    setPropertyValues({});
    setCustomPropertyPickerId(null);
    setProjectId(undefined);
    setParentIssueId(undefined);
    setStage(null);
    setChildIssues([]);
    setManual({
      title: '',
      description: '',
      status: 'todo',
      assigneeType,
      assigneeId,
      startDate: null,
      labelIds: [],
      propertyValues: {},
    });
    setShared({
      priority: 'none',
      projectId: undefined,
      dueDate: null,
      attachments: [],
    });
    descEditorRef.current?.clearContent();
    setFormResetKey((key) => key + 1);
  };

  const mountedRef = useRef(true);
  useLayoutEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedDraftRef = useRef<IssueCreateDraft | null>(null);

  const composer = useComposerSubmit({
    editorRef: descEditorRef,
    uploadGate: gate,
    normalize: () => title.trim(),
    onSubmit: async (): Promise<boolean> => {
      const pendingDesc = descEditorRef.current?.flushPendingUpdate?.();
      if (pendingDesc != null) setManual({ description: pendingDesc });
      submittedDraftRef.current = useIssueDraftStore.getState().draft;
      try {
        const description = descEditorRef.current?.getMarkdown()?.trim() || undefined;
        const activeAttachmentIds = draftAttachments
          .filter((a) => contentReferencesAttachment(description ?? '', a))
          .map((a) => a.id);
        const issue = await createIssueMutation.mutateAsync({
          title: title.trim(),
          description,
          status,
          priority,
          assignee_type: assigneeType,
          assignee_id: assigneeId,
          start_date: startDate || undefined,
          due_date: dueDate || undefined,
          attachment_ids: activeAttachmentIds.length > 0 ? activeAttachmentIds : undefined,
          label_ids: labelIds.length > 0 ? labelIds : undefined,
          parent_issue_id: parentIssueId,
          stage: parentIssueId && stage != null ? stage : undefined,
          project_id: projectId,
        });

        const propertyEntries = Object.entries(propertyValues);
        if (propertyEntries.length > 0) {
          const results = await Promise.allSettled(
            propertyEntries.map(([propertyId, value]) =>
              setIssuePropertyMutation.mutateAsync({
                issueId: issue.id,
                propertyId,
                value,
              }),
            ),
          );
          let failed = 0;
          for (const result of results) {
            if (result.status === 'rejected') {
              failed += 1;
              console.error('[create-issue] custom property set failed', result.reason);
            }
          }
          if (failed > 0) {
            toast.error(t(($) => $.create_issue.toast_set_properties_failed, { count: failed }));
          }
        }

        if (childIssues.length > 0) {
          const results = await Promise.allSettled(
            childIssues.map((child) =>
              updateIssueMutation.mutateAsync({
                id: child.id,
                parent_issue_id: issue.id,
              }),
            ),
          );
          for (const result of results) {
            if (result.status === 'rejected') {
              console.error('[create-issue] sub-issue link failed', result.reason);
            }
          }
          const failed = results.filter((r) => r.status === 'rejected').length;
          if (failed > 0) {
            toast.error(
              failed === childIssues.length
                ? t(($) => $.create_issue.toast_link_subissues_all_failed)
                : t(($) => $.create_issue.toast_link_subissues_partial, {
                    failed,
                    total: childIssues.length,
                  }),
            );
          }
        }

        if (labelIds.length > 0 && issue.labels === undefined) {
          const results = await Promise.allSettled(
            labelIds.map((labelId) =>
              attachLabelMutation.mutateAsync({ issueId: issue.id, labelId }),
            ),
          );
          let labelsFailed = 0;
          for (const result of results) {
            if (result.status === 'rejected') {
              labelsFailed += 1;
              console.error('[create-issue] label attach fallback failed', result.reason);
            }
          }
          if (labelsFailed > 0) {
            toast.error(t(($) => $.create_issue.toast_link_labels_failed));
          }
        }

        {
          toast.custom(
            (toastId) => (
              <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
                <div className="flex items-center gap-2 mb-2">
                  <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
                    <Check className="size-3" />
                  </div>
                  <span className="text-sm font-medium">
                    {t(($) => $.create_issue.toast_created)}
                  </span>
                </div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
                  <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
                  <span className="truncate">
                    {issue.identifier} – {issue.title}
                  </span>
                </div>
                <button
                  type="button"
                  className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
                  onClick={() => {
                    router.push(p.issueDetail(issue.id));
                    toast.dismiss(toastId);
                  }}
                >
                  {t(($) => $.create_issue.view_issue)}
                </button>
              </div>
            ),
            { duration: 5000 },
          );
        }
        return true;
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          const dup = parseWithFallback<DuplicateIssueErrorBody | null>(
            err.body,
            DuplicateIssueErrorBodySchema,
            null,
            { endpoint: 'POST /api/workspaces/:wsId/issues (active_duplicate_issue)' },
          );
          if (dup) {
            toast.custom(
              (toastId) => (
                <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
                  <div className="flex items-center gap-2 mb-2">
                    <div className="flex items-center justify-center size-5 rounded-full bg-warning/15 text-warning">
                      <AlertTriangle className="size-3" />
                    </div>
                    <span className="text-sm font-medium">
                      {t(($) => $.create_issue.toast_duplicate_title)}
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
                    <span className="truncate">
                      {dup.issue.identifier} – {dup.issue.title}
                    </span>
                  </div>
                  <button
                    type="button"
                    className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
                    onClick={() => {
                      router.push(p.issueDetail(dup.issue.id));
                      toast.dismiss(toastId);
                    }}
                  >
                    {t(($) => $.create_issue.toast_duplicate_view)}
                  </button>
                </div>
              ),
              { duration: 5000 },
            );
            return false;
          }
        }
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.create_issue.toast_failed),
        );
        return false;
      }
    },
    onAccepted: () => {
      setLastAssignee(assigneeType, assigneeId);
      setLastMode('manual');
      const lateDesc = descEditorRef.current?.flushPendingUpdate?.();
      if (lateDesc != null) setManual({ description: lateDesc });
      const untouched = useIssueDraftStore.getState().draft === submittedDraftRef.current;
      if (untouched) clearDraft();
      if (!mountedRef.current || !untouched) return;
      if (keepOpen) {
        resetForNextIssue();
      } else {
        onClose();
      }
    },
  });

  const handleSubmit = () => {
    if (!title.trim()) {
      titleEditorRef.current?.focus();
      return;
    }
    void composer.submit();
  };
  const submitting = composer.submitting;

  const switchToAgent = () => {
    if (gate.isBlocked()) return;
    setShared({ projectId, priority, dueDate });
    const existingPrompt = draft.agent.prompt;
    if (!existingPrompt.trim()) {
      const desc = descEditorRef.current?.getMarkdown()?.trim() ?? '';
      const seeded = [title.trim(), desc].filter(Boolean).join('\n\n');
      if (seeded) setAgent({ prompt: seeded });
    }
    if (
      !draft.agent.actorId &&
      assigneeId &&
      (assigneeType === 'agent' || assigneeType === 'squad')
    ) {
      setAgent({ actorType: assigneeType, actorId: assigneeId });
    }
    setLastMode('agent');
    setActiveMode('agent');
    const carryParentIdentifier =
      parentIssue?.identifier ?? (data?.parent_issue_identifier as string | undefined);
    const carry: Record<string, unknown> = {};
    if (parentIssueId) carry.parent_issue_id = parentIssueId;
    if (carryParentIdentifier) carry.parent_issue_identifier = carryParentIdentifier;
    onSwitchMode?.(Object.keys(carry).length > 0 ? carry : null);
  };

  const submitState: 'submitting' | 'uploading' | 'missing_title' | 'ready' = submitting
    ? 'submitting'
    : gate.uploading
      ? 'uploading'
      : !title.trim()
        ? 'missing_title'
        : 'ready';
  const submitBusy = submitState === 'submitting' || submitState === 'uploading';

  const createButton = (
    <Button
      size="sm"
      onClick={handleSubmit}
      disabled={submitBusy}
      aria-disabled={submitState === 'missing_title' || undefined}
      aria-busy={submitBusy || undefined}
      className="aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:active:translate-y-0"
    >
      {submitState === 'submitting' ? (
        t(($) => $.create_issue.submitting)
      ) : submitState === 'uploading' ? (
        tEditor(($) => $.upload.in_progress)
      ) : (
        <>
          {t(($) => $.create_issue.submit)}
          {/* Decorative: the accessible name must stay "Create Issue", not
              "Create Issue Command Enter". Absent when `send` is unbound. */}
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
  );

  return (
    <>
      <DialogTitle className="sr-only">{t(($) => $.create_issue.sr_manual)}</DialogTitle>

      {/* Header */}
      <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
        <div className="flex items-center gap-1.5 text-xs">
          <span className="text-muted-foreground">{workspaceName}</span>
          <ChevronRight className="size-3 text-muted-foreground/50" />
          <span className="font-medium">{t(($) => $.create_issue.manual_breadcrumb)}</span>
        </div>
        <div className="flex items-center gap-1">
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={() => setIsExpanded(!isExpanded)}
                  className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                >
                  {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                </button>
              }
            />
            <TooltipContent side="bottom">
              {isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)}
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={onClose}
                  className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                >
                  <XIcon className="size-4" />
                </button>
              }
            />
            <TooltipContent side="bottom">{t(($) => $.common.close)}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Title */}
      <div className="px-5 pb-2 shrink-0">
        <TitleEditor
          key={formResetKey}
          ref={titleEditorRef}
          autoFocus
          defaultValue={draft.manual.title}
          placeholder={t(($) => $.create_issue.title_placeholder)}
          className="text-lg font-semibold"
          onChange={(v) => updateTitle(v)}
          onSubmitShortcut={handleSubmit}
        />
      </div>

      {/* Description — takes remaining space */}
      <div {...descDropZoneProps} className="relative flex flex-1 min-h-0 overflow-y-auto px-5">
        <ContentEditor
          ref={descEditorRef}
          defaultValue={draft.manual.description}
          placeholder={t(($) => $.create_issue.description_placeholder)}
          onUpdate={(md) => setManual({ description: md })}
          onSubmit={handleSubmit}
          onUploadFile={handleUpload}
          onUploadingChange={uploadGate.onUploadingChange}
          debounceMs={500}
          attachments={draftAttachments}
        />
        {descDragOver && <FileDropOverlay />}
      </div>

      {/* Pre-trigger preview — a passive caption above the toolbar; reveals
                when an agent assignee will pick the issue up. */}
      <CreateRunHint assigneeType={assigneeType} assigneeId={assigneeId} status={status} />

      {/* Property toolbar — each field renders per the Settings → Issue
                selection (see showField above). */}
      <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
        {/* Status */}
        {showField.status && (
          <StatusPicker
            status={status}
            onUpdate={(u) => {
              if (u.status) updateStatus(u.status);
            }}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'status' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'status' : null)}
          />
        )}

        {/* Priority */}
        {showField.priority && (
          <PriorityPicker
            priority={priority}
            onUpdate={(u) => {
              if (u.priority) updatePriority(u.priority);
            }}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'priority' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'priority' : null)}
          />
        )}

        {/* Assignee */}
        {showField.assignee && (
          <AssigneePicker
            assigneeType={assigneeType ?? null}
            assigneeId={assigneeId ?? null}
            onUpdate={(u) =>
              updateAssignee(u.assignee_type ?? undefined, u.assignee_id ?? undefined)
            }
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'assignee' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'assignee' : null)}
          />
        )}

        {/* Labels — occupies the slot that used to hold Due date so the
                  add-label entry is exposed directly on the dialog. Draft mode:
                  selection is local until the issue is created (handleSubmit
                  attaches the labels afterward). */}
        {showField.labels && (
          <LabelPicker
            selectedIds={labelIds}
            onSelectedIdsChange={updateLabelIds}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'labels' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'labels' : null)}
          />
        )}

        {/* Project */}
        {showField.project && (
          <ProjectPicker
            projectId={projectId ?? null}
            onUpdate={(u) => updateProject(u.project_id ?? undefined)}
            triggerRender={<PillButton />}
            align="start"
            open={fieldPickerOpen === 'project' ? true : undefined}
            onOpenChange={(open) => setFieldPickerOpen(open ? 'project' : null)}
          />
        )}

        {/* Stage — only relevant when creating a sub-issue under a parent */}
        {parentIssueId && (
          <StagePicker
            stage={stage}
            onUpdate={(u) => setStage(u.stage ?? null)}
            maxStage={maxSiblingStage(parentChildren)}
            triggerRender={<PillButton />}
            align="start"
          />
        )}

        {/* Start date — collapsed into the ⋯ menu by default since it's
                  a low-frequency field (exposable via Settings → Issue).
                  Renders inline when configured visible, when the field has a
                  value, OR when the user just opened it from the overflow
                  menu (the picker's calendar popover needs the inline pill
                  as its anchor). */}
        {showField.start_date && (
          <StartDatePicker
            startDate={startDate}
            onUpdate={(u) => updateStartDate(u.start_date ?? null)}
            triggerRender={<PillButton />}
            align="start"
            open={startDatePickerOpen}
            onOpenChange={setStartDatePickerOpen}
          />
        )}

        {/* Due date — collapsed into the ⋯ menu by default (moved off
                  the toolbar to make room for Labels). Same reveal rule as
                  start date. */}
        {showField.due_date && (
          <DueDatePicker
            dueDate={dueDate}
            onUpdate={(u) => updateDueDate(u.due_date ?? null)}
            triggerRender={<PillButton />}
            align="start"
            open={dueDatePickerOpen}
            onOpenChange={setDueDatePickerOpen}
          />
        )}

        {/* Workspace-defined fields use the same typed editors as issue
                  detail, but write into the persisted draft until creation. */}
        {workspaceProperties
          .filter(
            (property) =>
              Object.prototype.hasOwnProperty.call(propertyValues, property.id) ||
              customPropertyPickerId === property.id,
          )
          .map((property) => {
            const value = propertyValues[property.id];
            return (
              <CustomPropertyValueInput
                key={property.id}
                property={property}
                value={value}
                onChange={(next) => updatePropertyValue(property.id, next)}
                open={customPropertyPickerId === property.id}
                onOpenChange={(open) => setCustomPropertyPickerId(open ? property.id : null)}
                triggerRender={<PillButton />}
                trigger={
                  <>
                    <PropertyIcon property={property} className="size-3.5 text-xs" />
                    <span className="max-w-32 truncate">{property.name}</span>
                    {value !== undefined && (
                      <span className="max-w-40 truncate text-muted-foreground">
                        <CustomPropertyValueDisplay property={property} value={value} />
                      </span>
                    )}
                  </>
                }
              />
            );
          })}

        {/* Parent chip — appears when parent is set.
                  Placed before the ⋯ so it wraps to a new line with ⋯ if
                  space is tight, but ⋯ always stays last in DOM order. */}
        {parentIssueId && parentIssue && (
          <div className="inline-flex items-center rounded-full border text-xs transition-colors hover:bg-accent/60">
            <button
              type="button"
              onClick={() => setParentPickerOpen(true)}
              className="flex items-center gap-1.5 py-1 pl-2.5 cursor-pointer"
            >
              <ArrowUp className="size-3 text-muted-foreground" />
              <span>
                {t(($) => $.create_issue.subissue_of, { identifier: parentIssue.identifier })}
              </span>
            </button>
            <button
              type="button"
              onClick={() => setParentIssueId(undefined)}
              className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
              aria-label={t(($) => $.create_issue.remove_parent_aria)}
            >
              <XIcon className="size-3" />
            </button>
          </div>
        )}

        {/* Child chips — one per queued sub-issue. Links are deferred
                  until create resolves (see handleSubmit). */}
        {childIssues.map((c) => (
          <div
            key={c.id}
            className="inline-flex items-center rounded-full border text-xs transition-colors hover:bg-accent/60"
          >
            <div className="flex items-center gap-1.5 py-1 pl-2.5">
              <ArrowDown className="size-3 text-muted-foreground" />
              <span>{t(($) => $.create_issue.subissue_chip, { identifier: c.identifier })}</span>
            </div>
            <button
              type="button"
              onClick={() => setChildIssues((prev) => prev.filter((x) => x.id !== c.id))}
              className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
              aria-label={t(($) => $.create_issue.remove_subissue_aria, {
                identifier: c.identifier,
              })}
            >
              <XIcon className="size-3" />
            </button>
          </div>
        ))}

        {/* Overflow — always the last child so DOM order keeps it at the
                  end of the wrap flow, no matter how many chips are present. */}
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <PillButton aria-label={t(($) => $.create_issue.more_options_aria)}>
                <MoreHorizontal className="size-3.5" />
              </PillButton>
            }
          />
          <DropdownMenuContent align="start" className="w-auto">
            {/* Re-entry points for toolbar fields hidden via
                      Settings → Issue. Listed in toolbar order; each opens
                      the picker inline (mounting the pill as its anchor). */}
            {!showField.status && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('status')}>
                <StatusIcon status={status} className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_status)}
              </DropdownMenuItem>
            )}
            {!showField.priority && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('priority')}>
                <PriorityIcon priority="none" className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_priority)}
              </DropdownMenuItem>
            )}
            {!showField.assignee && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('assignee')}>
                <CircleUser className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_assignee)}
              </DropdownMenuItem>
            )}
            {!showField.labels && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('labels')}>
                <Tag className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_labels)}
              </DropdownMenuItem>
            )}
            {!showField.project && (
              <DropdownMenuItem onClick={() => setFieldPickerOpen('project')}>
                <FolderKanban className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_project)}
              </DropdownMenuItem>
            )}
            {!showField.due_date && (
              <DropdownMenuItem onClick={() => setDueDatePickerOpen(true)}>
                <CalendarDays className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_due_date)}
              </DropdownMenuItem>
            )}
            {!showField.start_date && (
              <DropdownMenuItem onClick={() => setStartDatePickerOpen(true)}>
                <CalendarClock className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_start_date)}
              </DropdownMenuItem>
            )}
            {parentIssueId && parentIssue ? (
              <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                <ArrowUp className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.parent_with_id, { identifier: parentIssue.identifier })}
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                <ArrowUp className="h-3.5 w-3.5" />
                {t(($) => $.create_issue.set_parent)}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={() => setChildPickerOpen(true)}>
              <ArrowDown className="h-3.5 w-3.5" />
              {t(($) => $.create_issue.add_subissue)}
            </DropdownMenuItem>
            {workspaceProperties.length > 0 && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Shapes className="h-3.5 w-3.5" />
                  {t(($) => $.create_issue.custom_properties)}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-56">
                  {workspaceProperties.map((property) => (
                    <DropdownMenuItem
                      key={property.id}
                      disabled={Object.prototype.hasOwnProperty.call(propertyValues, property.id)}
                      onClick={() => setCustomPropertyPickerId(property.id)}
                    >
                      <PropertyIcon property={property} className="size-3.5 text-xs" />
                      <span className="truncate">{property.name}</span>
                      {Object.prototype.hasOwnProperty.call(propertyValues, property.id) && (
                        <Check className="ml-auto size-3.5" />
                      )}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={openFieldSettings}>
              <Settings2 className="h-3.5 w-3.5" />
              {t(($) => $.create_issue.customize_fields)}
            </DropdownMenuItem>
            {parentIssueId && parentIssue && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={() => setParentIssueId(undefined)}>
                  <XIcon className="h-3.5 w-3.5" />
                  {t(($) => $.create_issue.remove_parent)}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Parent / child pickers — rendered inline so they stack over this
                modal instead of replacing it via useModalStore. */}
      <IssuePickerModal
        open={parentPickerOpen}
        onOpenChange={setParentPickerOpen}
        title={t(($) => $.create_issue.set_parent_picker.title)}
        description={t(($) => $.create_issue.set_parent_picker.description)}
        excludeIds={[...childIssues.map((c) => c.id), ...(parentIssueId ? [parentIssueId] : [])]}
        onSelect={(selected) => {
          setParentIssueId(selected.id);
        }}
      />
      <IssuePickerModal
        open={childPickerOpen}
        onOpenChange={setChildPickerOpen}
        title={t(($) => $.create_issue.add_subissue_picker.title)}
        description={t(($) => $.create_issue.add_subissue_picker.description)}
        excludeIds={[...childIssues.map((c) => c.id), ...(parentIssueId ? [parentIssueId] : [])]}
        onSelect={(selected) => {
          setChildIssues((prev) =>
            prev.some((x) => x.id === selected.id) ? prev : [...prev, selected],
          );
        }}
      />

      {/* Footer */}
      <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-h-7 items-center gap-2">
          <FileUploadButton multiple onSelect={(file) => descEditorRef.current?.uploadFile(file)} />
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            onClick={switchToAgent}
            disabled={gate.uploading}
            aria-disabled={gate.uploading || undefined}
            aria-busy={gate.uploading || undefined}
            title={t(($) => $.create_issue.switch_to_agent_tooltip)}
            className="border-beam group flex shrink-0 items-center gap-1.5 text-xs px-2 py-1 rounded-sm text-muted-foreground bg-brand/5 hover:bg-brand/10 hover:text-foreground transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
          >
            <ArrowLeftRight className="size-3.5 text-brand/80 transition-transform duration-300 group-hover:rotate-180" />
            {t(($) => $.create_issue.switch_to_agent)}
          </button>
          <label className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
            <Switch size="sm" checked={keepOpen} onCheckedChange={setKeepOpen} />
            {t(($) => $.create_issue.create_another)}
          </label>
          {submitState === 'missing_title' ? (
            <TooltipProvider delay={200}>
              <Tooltip>
                {/* No `<span>` wrapper needed now: aria-disabled leaves the
                          button focusable and hoverable, so it can anchor its own
                          tooltip. */}
                <TooltipTrigger render={createButton} />
                <TooltipContent side="top">
                  {t(($) => $.create_issue.title_required)}
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
          ) : (
            createButton
          )}
        </div>
      </div>
    </>
  );
}

export function manualDialogContentClass(isExpanded: boolean) {
  return cn(
    'p-0 gap-0 flex flex-col overflow-hidden',
    '!top-1/2 !left-1/2 !-translate-x-1/2',
    '!transition-all !duration-300 !ease-out',
    isExpanded
      ? '!max-w-4xl !w-full !h-5/6 !-translate-y-1/2'
      : '!max-w-2xl !w-full !h-96 !-translate-y-1/2',
  );
}

import { Dialog as DialogRoot } from '@goosar/ui/components/ui/dialog';
export function CreateIssueModal(props: {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}) {
  const [isExpanded, setIsExpanded] = useState(false);
  return (
    <DialogRoot
      open
      onOpenChange={(v) => {
        if (!v) props.onClose();
      }}
    >
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={manualDialogContentClass(isExpanded)}
      >
        <ManualCreatePanel {...props} isExpanded={isExpanded} setIsExpanded={setIsExpanded} />
      </DialogContent>
    </DialogRoot>
  );
}

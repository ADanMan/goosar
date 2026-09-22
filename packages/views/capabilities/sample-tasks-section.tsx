'use client';

import { useState } from 'react';
import { KeyRound, Loader2, Play, TriangleAlert } from 'lucide-react';
import { useCreateIssue } from '@goosar/core/issues/mutations';
import { useWorkspacePaths } from '@goosar/core/paths';
import type { WorkspaceSampleTask } from '@goosar/core/types';
import { Button } from '@goosar/ui/components/ui/button';
import { useNavigation } from '../navigation';
import { workToolsPresetText } from '../onboarding/presets';
import type { HelperMcpPresetName } from '../onboarding/presets';
import { useT } from '../i18n';
import { sampleTaskBlockers } from './sample-task-gate';
import type { ServiceCredentialsView } from './use-service-credentials';

type RunState =
  { kind: 'idle' } | { kind: 'running' } | { kind: 'done'; identifier: string } | { kind: 'error' };

export function SampleTasksSection({
  tasks,
  credentials,
}: {
  tasks: readonly WorkspaceSampleTask[];
  credentials: ServiceCredentialsView;
}) {
  const { t } = useT('workspace');
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const createIssue = useCreateIssue();
  const [runs, setRuns] = useState<Record<string, RunState>>({});

  if (tasks.length === 0) return null;

  const helper = credentials.helper;

  const idOf = (task: WorkspaceSampleTask, index: number) => task.key || `#${index}`;

  const run = async (task: WorkspaceSampleTask, id: string) => {
    if (!helper) return;
    setRuns((prev) => ({ ...prev, [id]: { kind: 'running' } }));
    try {
      const issue = await createIssue.mutateAsync({
        title: task.title,
        description: task.prompt,
        status: 'todo',
        priority: 'high',
        assignee_type: 'agent',
        assignee_id: helper.id,
      });
      setRuns((prev) => ({
        ...prev,
        [id]: { kind: 'done', identifier: issue.identifier },
      }));
      openIssue(issue.id, issue.identifier);
    } catch {
      setRuns((prev) => ({ ...prev, [id]: { kind: 'error' } }));
    }
  };

  const openIssue = (id: string, identifier: string) => {
    const path = paths.issueDetail(id);
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, identifier, { activate: true });
      return;
    }
    navigation.push(path);
  };

  return (
    <section className="mt-10" aria-label={t(($) => $.capabilities.sample_tasks_heading)}>
      <h2 className="text-[15px] font-medium text-foreground">
        {t(($) => $.capabilities.sample_tasks_heading)}
      </h2>
      <p className="mt-1 max-w-[600px] text-[12.5px] leading-relaxed text-muted-foreground">
        {t(($) => $.capabilities.sample_tasks_lede)}
      </p>
      <ul className="mt-4 flex flex-col gap-3">
        {tasks.map((task, index) => (
          <SampleTaskCard
            key={idOf(task, index)}
            task={task}
            blockers={sampleTaskBlockers(task, credentials.statuses)}
            helperReady={!!helper}
            state={runs[idOf(task, index)] ?? { kind: 'idle' }}
            onRun={() => void run(task, idOf(task, index))}
            onOpenSetup={
              helper
                ? () => navigation.push(`${paths.agentDetail(helper.id)}?view=mcp_config`)
                : undefined
            }
          />
        ))}
      </ul>
    </section>
  );
}

SampleTasksSection.displayName = 'SampleTasksSection';

function SampleTaskCard({
  task,
  blockers,
  helperReady,
  state,
  onRun,
  onOpenSetup,
}: {
  task: WorkspaceSampleTask;
  blockers: readonly HelperMcpPresetName[];
  helperReady: boolean;
  state: RunState;
  onRun: () => void;
  onOpenSetup?: () => void;
}) {
  const { t } = useT('workspace');
  const { t: tOnboarding } = useT('onboarding');
  const blocked = blockers.length > 0;
  const services = blockers
    .map((preset) => workToolsPresetText(tOnboarding, preset).title)
    .join(', ');

  return (
    <li className="rounded-lg border bg-card p-4">
      <p className="text-[14px] font-medium text-foreground">{task.title}</p>
      <p className="mt-1 line-clamp-3 text-[12.5px] leading-[1.55] text-muted-foreground">
        {task.prompt}
      </p>

      {blocked ? (
        <div className="mt-3 rounded-md border border-warning/40 bg-warning/5 p-3">
          <p className="flex items-center gap-2 text-[12.5px] font-medium text-foreground">
            <KeyRound className="h-3.5 w-3.5 text-warning" aria-hidden />
            {t(($) => $.capabilities.sample_task_needs_key, { services })}
          </p>
          <p className="mt-1 text-[12px] leading-relaxed text-muted-foreground">
            {t(($) => $.capabilities.sample_task_needs_key_body)}
          </p>
          {onOpenSetup ? (
            <button
              type="button"
              className="mt-2 text-[12.5px] font-medium text-primary underline-offset-4 hover:underline"
              onClick={onOpenSetup}
            >
              {t(($) => $.capabilities.open_setup)}
            </button>
          ) : null}
        </div>
      ) : !helperReady ? (
        <p className="mt-3 text-[12px] leading-relaxed text-muted-foreground">
          {t(($) => $.capabilities.sample_task_no_helper)}
        </p>
      ) : (
        <div className="mt-3 flex flex-wrap items-center gap-3">
          <Button size="sm" variant="outline" disabled={state.kind === 'running'} onClick={onRun}>
            {state.kind === 'running' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            ) : (
              <Play className="h-3.5 w-3.5" aria-hidden />
            )}
            {state.kind === 'done'
              ? t(($) => $.capabilities.sample_task_run_again)
              : t(($) => $.capabilities.sample_task_run)}
          </Button>
          {state.kind === 'done' ? (
            <span className="text-[12px] text-muted-foreground">
              {t(($) => $.capabilities.sample_task_created, {
                identifier: state.identifier,
              })}
            </span>
          ) : null}
          {state.kind === 'error' ? (
            <span className="flex items-center gap-1.5 text-[12px] text-destructive">
              <TriangleAlert className="h-3.5 w-3.5" aria-hidden />
              {t(($) => $.capabilities.sample_task_failed)}
            </span>
          ) : null}
        </div>
      )}
    </li>
  );
}

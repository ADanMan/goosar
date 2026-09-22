'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { toastApiError } from '../common/error-toast';
import { setCurrentWorkspace } from '@goosar/core/platform';
import { useAuthStore } from '@goosar/core/auth';
import {
  completeOnboarding,
  hasPendingOnboardingCompletion,
  ONBOARDING_STEP_ORDER,
  saveQuestionnaire,
  useWelcomeStore,
  type OnboardingStep,
  type QuestionnaireAnswers,
} from '@goosar/core/onboarding';
import { joinTargetListOptions, workspaceListOptions } from '@goosar/core/workspace/queries';
import { effectiveConfigOptions } from '@goosar/core/workspace/effective-config';
import type { AgentRuntime, Workspace } from '@goosar/core/types';
import { StepWelcome } from './steps/step-welcome';
import { StepAboutYou } from './steps/step-about-you';
import { StepWorkspace } from './steps/step-workspace';
import { StepChooseRole } from './steps/step-choose-role';
import {
  StepPrepareWorkspace,
  type LlmGatewayRowStatus,
  type PxProxyRowStatus,
} from './steps/step-prepare-workspace';
import { StepRuntimeConnect } from './steps/step-runtime-connect';
import type { SaveLlmConnection } from './components/llm-connection-form';
import type { PerimeterMachineState } from './perimeter-machine';
import type { LlmExistingConnectionInfo } from './components/llm-connection-form';
import type { ProvisioningStatus } from './provisioning-status';
import type { AgentRuntimeStatus } from './agent-row-status';
import { StepCompletionRecovery } from './steps/step-completion-recovery';
import { StepPlatformFork } from './steps/step-platform-fork';
import { StepWorkTools } from './steps/step-work-tools';
import { PUBLIC_INDEX_PRESETS } from './presets';
import { useT } from '../i18n';

const EMPTY_QUESTIONNAIRE: QuestionnaireAnswers = {
  source: [],
  source_other: null,
  source_skipped: false,
  role: null,
  role_other: null,
  role_skipped: false,
  use_case: [],
  use_case_other: null,
  use_case_skipped: false,
  version: 2,
};

function coerceToArray<T extends string>(value: unknown): T[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is T => typeof v === 'string' && v.length > 0);
  }
  if (typeof value === 'string' && value.length > 0) {
    return [value as T];
  }
  return [];
}

function mergeQuestionnaire(raw: Record<string, unknown>): QuestionnaireAnswers {
  const merged = {
    ...EMPTY_QUESTIONNAIRE,
    ...(raw as Partial<QuestionnaireAnswers>),
  };
  return {
    ...merged,
    source: coerceToArray<QuestionnaireAnswers['source'][number]>(raw.source),
    use_case: coerceToArray<QuestionnaireAnswers['use_case'][number]>(raw.use_case),
    source_skipped: false,
    role_skipped: false,
    use_case_skipped: false,
  };
}

export function OnboardingFlow({
  onComplete,
  runtimeInstructions,
  onRuntimeRefresh,
  runtimesPending,
  onSaveLlmConnection,
  localDaemonId,
  localMachineName,
  perimeterMachine,
  existingLlmConnection,
  daemonState,
  networkStatusSlot,
  provisioningStatus,
  onRetryProvisioning,
  onWorkspaceProvisioning,
  agentStatus,
  onRetryAgent,
  onSkipAgent,
  llmGateway,
  onRetryLlmGateway,
  pxProxy,
}: {
  onComplete: (workspace?: Workspace, issueId?: string) => void;
  runtimeInstructions?: React.ReactNode;
  onRuntimeRefresh?: () => void | Promise<void>;
  runtimesPending?: boolean;
  onSaveLlmConnection?: SaveLlmConnection;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  perimeterMachine?: PerimeterMachineState;
  existingLlmConnection?: LlmExistingConnectionInfo | null;
  daemonState?: string | null;
  networkStatusSlot?: React.ReactNode;
  provisioningStatus?: ProvisioningStatus | null;
  onRetryProvisioning?: () => void | Promise<void>;
  onWorkspaceProvisioning?: (workspaceId: string) => void;
  agentStatus?: AgentRuntimeStatus | null;
  onRetryAgent?: () => void | Promise<void>;
  onSkipAgent?: () => void;
  llmGateway?: LlmGatewayRowStatus | null;
  onRetryLlmGateway?: () => void | Promise<void>;
  pxProxy?: PxProxyRowStatus | null;
}) {
  const { t } = useT('onboarding');
  const queryClient = useQueryClient();
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error('OnboardingFlow requires an authenticated user');
  }

  const storedQuestionnaire = mergeQuestionnaire(user.onboarding_questionnaire);
  const [answers, setAnswers] = useState<QuestionnaireAnswers>(storedQuestionnaire);

  const [step, setStep] = useState<OnboardingStep>('welcome');
  const [recoveringCompletion, setRecoveringCompletion] = useState(
    () => user.onboarded_at == null && hasPendingOnboardingCompletion(user.id),
  );
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [pickedRuntime, setPickedRuntime] = useState<AgentRuntime | null>(null);
  const [showPrepareWorkspace, setShowPrepareWorkspace] = useState(false);
  const [joinedRole, setJoinedRole] = useState(false);
  const [createInstead, setCreateInstead] = useState(false);

  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: step === 'welcome' || step === 'workspace',
  });
  const existingWorkspace = workspace ?? workspaces[0] ?? null;

  const { data: joinTargets = [] } = useQuery({
    ...joinTargetListOptions(),
    enabled: step === 'workspace',
  });
  const showRolePicker = !existingWorkspace && !createInstead && joinTargets.length > 0;
  const canSkipWelcome = workspacesFetched && workspaces.length > 0;

  const isWeb = !!runtimeInstructions;

  const hasPrepareWorkspaceStep = !!onRetryProvisioning;

  const installedNames = !hasPrepareWorkspaceStep
    ? undefined
    : (provisioningStatus?.packages ?? [])
        .filter((pkg) => pkg.type === 'mcp-server' && pkg.state === 'ok')
        .map((pkg) => pkg.name);

  const effectiveConfigQuery = useQuery({
    ...effectiveConfigOptions(workspace?.id ?? ''),
    enabled: joinedRole && !!workspace?.id && step === 'work_tools',
  });
  const roleMcpNames =
    !joinedRole || !effectiveConfigQuery.isSuccess
      ? undefined
      : [
          ...Object.entries(effectiveConfigQuery.data?.mcp ?? {})
            .filter(([, view]) => view.enabled === true)
            .map(([name]) => name),
          ...PUBLIC_INDEX_PRESETS,
        ];

  const installedMcpNames =
    roleMcpNames === undefined
      ? installedNames
      : installedNames === undefined
        ? roleMcpNames
        : installedNames.filter((name) => roleMcpNames.includes(name));

  const nextStep = useCallback((from: OnboardingStep): OnboardingStep | null => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx < 0 || idx >= ONBOARDING_STEP_ORDER.length - 1) return null;
    return ONBOARDING_STEP_ORDER[idx + 1]!;
  }, []);

  const advanceFrom = useCallback(
    (from: OnboardingStep) => {
      const next = nextStep(from);
      if (next) setStep(next);
    },
    [nextStep],
  );

  const handleWelcomeNext = useCallback(() => {
    setStep(ONBOARDING_STEP_ORDER[0]!);
  }, []);

  const applyAnswers = useCallback(
    (patch: Partial<QuestionnaireAnswers>) => {
      setAnswers((a) => {
        const next = { ...a, ...patch };
        void saveQuestionnaire(next).catch((err) => {
          toastApiError(
            err,
            t(($) => $.errors.save_failed),
          );
        });
        return next;
      });
    },
    [t],
  );

  const handleWelcomeSkip = useCallback(async () => {
    try {
      await completeOnboarding('skip_existing', workspaces[0]?.id);
    } catch (err) {
      toastApiError(
        err,
        t(($) => $.errors.skip_failed),
      );
      return;
    }
    onComplete(workspaces[0] ?? undefined);
  }, [workspaces, onComplete, t]);

  const handleWorkspaceCreated = useCallback(
    (ws: Workspace, viaRole = false) => {
      setWorkspace(ws);
      setJoinedRole(viaRole);
      setCurrentWorkspace(ws.slug, ws.id);
      onWorkspaceProvisioning?.(ws.id);
      if (hasPrepareWorkspaceStep) {
        setShowPrepareWorkspace(true);
        return;
      }
      advanceFrom('workspace');
    },
    [advanceFrom, hasPrepareWorkspaceStep, onWorkspaceProvisioning],
  );

  const handlePrepareWorkspaceAdvance = useCallback(() => {
    setShowPrepareWorkspace(false);
    advanceFrom('workspace');
  }, [advanceFrom]);

  const handleRuntimeNext = useCallback(
    async (rt: AgentRuntime | null) => {
      if (!workspace) return;
      try {
        await completeOnboarding(rt ? 'full' : 'runtime_skipped', workspace.id);
      } catch (err) {
        toastApiError(
          err,
          t(($) => $.errors.skip_failed),
        );
        return;
      }
      useWelcomeStore.getState().set({
        workspaceId: workspace.id,
        choice: rt ? 'runtime' : 'skip',
        ...(rt ? { runtimeId: rt.id } : {}),
      });
      if (rt) {
        setPickedRuntime(rt);
        advanceFrom('runtime');
        return;
      }
      onComplete(workspace, undefined);
    },
    [workspace, advanceFrom, onComplete, t],
  );

  const handleWorkToolsFinish = useCallback(() => {
    console.assert(workspace !== null, 'OnboardingFlow: work_tools finished without a workspace');
    if (!workspace) return;
    onComplete(workspace, undefined);
  }, [workspace, onComplete]);

  const handleRecovered = useCallback(async () => {
    let list = workspaces;
    if (list.length === 0) {
      try {
        list = await queryClient.fetchQuery(workspaceListOptions());
      } catch {
        // The completion IS confirmed at this point; a failed list read is
        // not a reason to hold the person on the recovery screen. Fall
        // through with what we have and let the destination re-fetch.
      }
    }
    if (!mountedRef.current) return;
    onComplete(list[0] ?? undefined);
  }, [workspaces, queryClient, onComplete]);

  const handleBack = useCallback((from: OnboardingStep) => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx <= 0) {
      setStep('welcome');
      return;
    }
    const prev = ONBOARDING_STEP_ORDER[idx - 1]!;
    setStep(prev);
  }, []);

  if (recoveringCompletion) {
    return (
      <StepCompletionRecovery
        onRecovered={handleRecovered}
        onStartOver={() => setRecoveringCompletion(false)}
      />
    );
  }

  if (step === 'welcome') {
    return (
      <StepWelcome
        onNext={handleWelcomeNext}
        onSkip={canSkipWelcome ? handleWelcomeSkip : undefined}
        isWeb={isWeb}
      />
    );
  }

  if (step === 'about_you') {
    return (
      <StepAboutYou
        answers={answers}
        onChange={applyAnswers}
        onAdvance={() => advanceFrom('about_you')}
        onSkip={() => advanceFrom('about_you')}
        onBack={() => handleBack('about_you')}
      />
    );
  }

  if (step === 'workspace') {
    if (showPrepareWorkspace && workspace) {
      return (
        <StepPrepareWorkspace
          status={provisioningStatus ?? null}
          onRetry={onRetryProvisioning!}
          onAdvance={handlePrepareWorkspaceAdvance}
          onBack={() => setShowPrepareWorkspace(false)}
          agentStatus={agentStatus ?? null}
          onRetryAgent={onRetryAgent}
          onSkipAgent={onSkipAgent}
          llmGateway={llmGateway ?? null}
          onRetryLlmGateway={onRetryLlmGateway}
          pxProxy={pxProxy ?? null}
        />
      );
    }
    if (showRolePicker) {
      return (
        <StepChooseRole
          onJoined={(ws) => handleWorkspaceCreated(ws, true)}
          onBack={() => handleBack('workspace')}
          onCreateInstead={() => setCreateInstead(true)}
        />
      );
    }
    return (
      <StepWorkspace
        existing={existingWorkspace}
        onCreated={handleWorkspaceCreated}
        onBack={() => handleBack('workspace')}
      />
    );
  }

  if (step === 'runtime' && workspace) {
    if (!runtimeInstructions) {
      return (
        <StepRuntimeConnect
          wsId={workspace.id}
          onNext={handleRuntimeNext}
          onBack={() => handleBack('runtime')}
          onRefresh={onRuntimeRefresh}
          runtimesPending={runtimesPending}
          onSaveLlmConnection={onSaveLlmConnection}
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          perimeterMachine={perimeterMachine}
          existingLlmConnection={existingLlmConnection}
          daemonState={daemonState}
          networkStatusSlot={networkStatusSlot}
        />
      );
    }
    return (
      <StepPlatformFork
        wsId={workspace.id}
        onNext={handleRuntimeNext}
        onBack={() => handleBack('runtime')}
        cliInstructions={runtimeInstructions}
      />
    );
  }

  if (step === 'work_tools' && workspace && pickedRuntime) {
    return (
      <StepWorkTools
        wsId={workspace.id}
        runtime={pickedRuntime}
        onFinish={handleWorkToolsFinish}
        onBack={() => handleBack('work_tools')}
        perimeterCaMissing={perimeterMachine?.caBundleMissing === true}
        installedMcpNames={installedMcpNames}
      />
    );
  }

  return null;
}

export type { OnboardingStep };

import { mkdir, readFile, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';
import {
  AGENT_RUNNER_CHOICES,
  DEFAULT_AGENT_RUNNER,
  type AgentRunnerChoice,
} from '../shared/agent-runtime-types';

const PREFERENCE_KEY = 'agentRunner';

export function normalizeAgentRunner(value: unknown): AgentRunnerChoice {
  return AGENT_RUNNER_CHOICES.find((choice) => choice === value) ?? DEFAULT_AGENT_RUNNER;
}

async function readPreferencesObject(filePath: string): Promise<Record<string, unknown>> {
  try {
    const parsed: unknown = JSON.parse(await readFile(filePath, 'utf-8'));
    return typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

export async function loadAgentRunner(filePath: string): Promise<AgentRunnerChoice> {
  const stored = await readPreferencesObject(filePath);
  return normalizeAgentRunner(stored[PREFERENCE_KEY]);
}

export async function saveAgentRunner(filePath: string, choice: AgentRunnerChoice): Promise<void> {
  const stored = await readPreferencesObject(filePath);
  stored[PREFERENCE_KEY] = normalizeAgentRunner(choice);
  await mkdir(dirname(filePath), { recursive: true });
  const temporaryPath = `${filePath}.tmp`;
  await writeFile(temporaryPath, JSON.stringify(stored, null, 2), 'utf-8');
  await rename(temporaryPath, filePath);
}

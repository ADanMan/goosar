import type { AgentConfigSaveResult } from './agent-runtime-types';
import type { LlmAuthReachability } from './perimeter-config';

export interface LlmRuntimeConfigView {
  apiBase: string | null;
  model: string | null;
  hasKey: boolean;
  configPath: string | null;
}

export interface LlmRuntimeSaveInput {
  apiBase: string;
  model: string;
}

export type LlmRuntimeSaveRejection =
  | { reason: 'empty'; message: string }
  /** The endpoint is not a valid http(s) URL. */
  | { reason: 'invalid_api_base'; message: string };

export type LlmRuntimeSaveResult =
  | { status: 'rejected'; rejection: LlmRuntimeSaveRejection }
  | {
      status: 'written';
      saved: AgentConfigSaveResult;
      reachability: LlmAuthReachability | null;
    };

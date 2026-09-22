// Централизованный построитель URL-путей. Вся навигация в общих пакетах
// идёт через него: пути воркспейса и глобальные пути.

const encode = (id: string) => encodeURIComponent(id);

function workspaceScoped(slug: string) {
  const ws = `/${encode(slug)}`;
  return {
    root: () => `${ws}/issues`,
    usage: () => `${ws}/usage`,
    issues: () => `${ws}/issues`,
    issueDetail: (id: string) => `${ws}/issues/${encode(id)}`,
    projects: () => `${ws}/projects`,
    projectDetail: (id: string) => `${ws}/projects/${encode(id)}`,
    autopilots: () => `${ws}/autopilots`,
    autopilotDetail: (id: string) => `${ws}/autopilots/${encode(id)}`,
    agents: () => `${ws}/agents`,
    newAgent: () => `${ws}/agents/new`,
    agentDetail: (id: string) => `${ws}/agents/${encode(id)}`,
    memberDetail: (id: string) => `${ws}/members/${encode(id)}`,
    squads: () => `${ws}/squads`,
    squadDetail: (id: string) => `${ws}/squads/${encode(id)}`,
    inbox: () => `${ws}/inbox`,
    chat: () => `${ws}/chat`,
    myIssues: () => `${ws}/my-issues`,
    runtimes: () => `${ws}/runtimes`,
    runtimeDetail: (id: string) => `${ws}/runtimes/${encode(id)}`,
    runtimeSettings: (machineId: string, runtimeId: string) =>
      `${ws}/runtimes/${encode(machineId)}/runtime/${encode(runtimeId)}`,
    skills: () => `${ws}/skills`,
    skillDetail: (id: string) => `${ws}/skills/${encode(id)}`,
    settings: () => `${ws}/settings`,
    capabilities: () => `${ws}/capabilities`,
    attachmentPreview: (id: string) => `${ws}/attachments/${encode(id)}/preview`,
  };
}

export const ONBOARDING_REPLAY_PARAM = 'replay';
export const ONBOARDING_REPLAY_VALUE = '1';

export const paths = {
  workspace: workspaceScoped,

  login: () => '/login',
  newWorkspace: () => '/workspaces/new',
  joinWorkspace: () => '/workspaces/join',
  invite: (id: string) => `/invite/${encode(id)}`,
  invitations: () => '/invitations',
  onboarding: () => '/onboarding',
  onboardingReplay: () => `/onboarding?${ONBOARDING_REPLAY_PARAM}=${ONBOARDING_REPLAY_VALUE}`,
  authCallback: () => '/auth/callback',
  root: () => '/',
};

export type WorkspacePaths = ReturnType<typeof workspaceScoped>;

const GLOBAL_PREFIXES = [
  '/login',
  '/workspaces/',
  '/invite/',
  '/invitations',
  '/onboarding',
  '/auth/',
  '/logout',
  '/signup',
];

export function isGlobalPath(path: string): boolean {
  return GLOBAL_PREFIXES.some((p) => path === p || path.startsWith(p));
}

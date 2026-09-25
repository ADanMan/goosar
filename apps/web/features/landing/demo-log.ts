/**
 * Static scenario for the landing-page terminal feed (T-012).
 * No network calls: every line is a fixed i18n string, rendered through
 * `packages/ui/components/ui/terminal.tsx` (Magic UI Terminal, MIT).
 */
export type DemoLogLineKind = 'task' | 'command' | 'output' | 'status';

export interface DemoLogLine {
  kind: DemoLogLineKind;
  /** Key into `LandingDict['demoLog']['lines']`. */
  key: DemoLogLineKey;
}

export const demoLogLineKeys = [
  'taskCreated',
  'agentAssigned',
  'agentReadingCode',
  'agentFoundCause',
  'agentWritingTest',
  'testWritten',
  'agentRunningTests',
  'testsFailedFirst',
  'testsPassed',
  'agentOpeningPr',
  'prOpened',
  'statusInReview',
] as const;

export type DemoLogLineKey = (typeof demoLogLineKeys)[number];

export const demoLog: DemoLogLine[] = [
  { kind: 'task', key: 'taskCreated' },
  { kind: 'command', key: 'agentAssigned' },
  { kind: 'output', key: 'agentReadingCode' },
  { kind: 'output', key: 'agentFoundCause' },
  { kind: 'command', key: 'agentWritingTest' },
  { kind: 'output', key: 'testWritten' },
  { kind: 'command', key: 'agentRunningTests' },
  { kind: 'output', key: 'testsFailedFirst' },
  { kind: 'output', key: 'testsPassed' },
  { kind: 'command', key: 'agentOpeningPr' },
  { kind: 'output', key: 'prOpened' },
  { kind: 'status', key: 'statusInReview' },
];

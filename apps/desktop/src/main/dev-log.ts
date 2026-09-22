export type DevLog = (tag: string, ...args: unknown[]) => void;

type DevLogSink = {
  readonly destroyed?: boolean;
  readonly writable?: boolean;
  on: (event: 'error', listener: (error: Error) => void) => unknown;
  write: (message: string) => unknown;
};

export function createBestEffortDevLog(sink: DevLogSink = process.stderr): DevLog {
  sink.on('error', () => undefined);

  return (tag, ...args) => {
    if (sink.destroyed === true || sink.writable === false) return;
    try {
      sink.write(`[renderer ${tag}] ${args.map(String).join(' ')}\n`);
    } catch {
      // Some sinks fail synchronously once their launcher has disappeared.
    }
  };
}

import { describe, expect, it } from 'vitest';
import { isDaemonReadinessTimeout } from './daemon-start-verdict';

describe('isDaemonReadinessTimeout', () => {
  const readiness =
    'daemon did not confirm readiness within 45s (agent detection / workspace sync is taking longer than expected). It may still come up; check:\n  /x/daemon.log';

  it("recognises the CLI's readiness timeout: exit 2 plus its message", () => {
    expect(isDaemonReadinessTimeout({ code: 2 }, readiness)).toBe(true);
    expect(isDaemonReadinessTimeout({ code: 2 }, Buffer.from(readiness))).toBe(true);
  });

  it('does not swallow real failures that share the exit code', () => {
    expect(
      isDaemonReadinessTimeout({ code: 2 }, 'daemon failed to start: cannot reach the server'),
    ).toBe(false);
  });

  it('does not match other exit codes or a clean exit', () => {
    expect(isDaemonReadinessTimeout({ code: 1 }, readiness)).toBe(false);
    expect(isDaemonReadinessTimeout(null, readiness)).toBe(false);
    expect(isDaemonReadinessTimeout({ code: null }, readiness)).toBe(false);
  });
});

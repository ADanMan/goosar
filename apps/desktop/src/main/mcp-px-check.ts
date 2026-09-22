// Онбординг-проверка «MCP стартует через px»: запускает настроенный stdio-MCP
// сервер с прокси px, шлёт JSON-RPC initialize и ждёт ответ. Выполняется только
// на шаге установки пакетов, не в goosar doctor.

export const MCP_PX_CHECK_TIMEOUT_MS = 20_000;

export type McpPxCheckStatus = 'ok' | 'proxy_unreachable' | string;

export interface McpPxCheckChild {
  stdin: { write: (data: string) => void };
  onStdout: (cb: (chunk: string) => void) => void;
  onStderr: (cb: (chunk: string) => void) => void;
  onExit: (cb: (code: number | null) => void) => void;
  onError: (cb: (err: Error) => void) => void;
  kill: () => void;
}

export interface McpPxCheckDeps {
  spawn: (command: string, args: string[], env: NodeJS.ProcessEnv) => McpPxCheckChild;
}

const PX_PROXY_URL = 'http://127.0.0.1:3128';

function looksLikeProxyFailure(line: string): boolean {
  const lower = line.toLowerCase();
  return (
    lower.includes('econnrefused') ||
    lower.includes('proxyerror') ||
    (lower.includes('proxy') && lower.includes('refused'))
  );
}

function initializeRequest(): string {
  return (
    JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: {
        protocolVersion: '2024-11-05',
        capabilities: {},
        clientInfo: { name: 'goosar-onboarding', version: '1' },
      },
    }) + '\n'
  );
}

function isInitializeResult(line: string): boolean {
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    return false;
  }
  if (typeof parsed !== 'object' || parsed === null) return false;
  const obj = parsed as Record<string, unknown>;
  return obj.id === 1 && 'result' in obj && !('error' in obj);
}

export function checkMcpThroughPx(
  command: string,
  args: string[],
  deps: McpPxCheckDeps,
  env: NodeJS.ProcessEnv = process.env,
  timeoutMs: number = MCP_PX_CHECK_TIMEOUT_MS,
): Promise<McpPxCheckStatus> {
  return new Promise((resolve) => {
    let settled = false;
    let stdoutBuf = '';
    let lastStderrLine = '';
    let timer: ReturnType<typeof setTimeout> | null = null;

    const finish = (status: McpPxCheckStatus): void => {
      if (settled) return;
      settled = true;
      if (timer) clearTimeout(timer);
      try {
        child.kill();
      } catch {
        // Best-effort: the child may already be gone.
      }
      resolve(status);
    };

    const child = deps.spawn(command, args, {
      ...env,
      HTTPS_PROXY: PX_PROXY_URL,
      HTTP_PROXY: PX_PROXY_URL,
    });

    child.onStdout((chunk) => {
      stdoutBuf += chunk;
      let newlineIdx: number;
      while ((newlineIdx = stdoutBuf.indexOf('\n')) !== -1) {
        const line = stdoutBuf.slice(0, newlineIdx).trim();
        stdoutBuf = stdoutBuf.slice(newlineIdx + 1);
        if (line && isInitializeResult(line)) {
          finish('ok');
          return;
        }
      }
    });

    child.onStderr((chunk) => {
      const lines = chunk
        .split('\n')
        .map((l) => l.trim())
        .filter(Boolean);
      if (lines.length > 0) lastStderrLine = lines[lines.length - 1];
    });

    child.onError((err) => {
      finish(
        looksLikeProxyFailure(err.message) ? 'proxy_unreachable' : `mcp_failed(${err.message})`,
      );
    });

    child.onExit(() => {
      finish(
        lastStderrLine
          ? looksLikeProxyFailure(lastStderrLine)
            ? 'proxy_unreachable'
            : `mcp_failed(${lastStderrLine})`
          : 'mcp_failed(exited before responding)',
      );
    });

    timer = setTimeout(() => {
      finish(
        lastStderrLine && looksLikeProxyFailure(lastStderrLine)
          ? 'proxy_unreachable'
          : 'mcp_failed(server did not respond within 20s)',
      );
    }, timeoutMs);

    child.stdin.write(initializeRequest());
  });
}

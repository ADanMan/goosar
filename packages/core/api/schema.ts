import type { ZodType } from 'zod';
import { type Logger, noopLogger } from '../logger';

let schemaLogger: Logger = noopLogger;

export function setSchemaLogger(logger: Logger): void {
  schemaLogger = logger;
}

export interface ParseOptions {
  endpoint: string;
  omitReceivedFromLog?: boolean;
}

function mismatchLogPayload(
  data: unknown,
  issues: unknown,
  opts: ParseOptions,
): Record<string, unknown> {
  const base: Record<string, unknown> = { endpoint: opts.endpoint, issues };
  if (opts.omitReceivedFromLog === true) {
    base.received = '[omitted: response carries secret values]';
    return base;
  }
  base.received = data;
  return base;
}

export function parseWithFallback<T>(
  data: unknown,
  schema: ZodType,
  fallback: T,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  schemaLogger.warn(
    `API response failed schema validation: ${opts.endpoint}`,
    mismatchLogPayload(data, result.error.issues, opts),
  );
  return fallback;
}

export class SchemaMismatchError extends Error {
  readonly endpoint: string;
  readonly issues: unknown;

  constructor(endpoint: string, issues: unknown) {
    super(`API response failed schema validation: ${endpoint}`);
    this.name = 'SchemaMismatchError';
    this.endpoint = endpoint;
    this.issues = issues;
  }
}

export function parseStrict<T>(data: unknown, schema: ZodType, opts: ParseOptions): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  schemaLogger.warn(
    `API response failed schema validation: ${opts.endpoint}`,
    mismatchLogPayload(data, result.error.issues, opts),
  );
  throw new SchemaMismatchError(opts.endpoint, result.error.issues);
}

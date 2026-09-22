// Мобильный parseWithFallback — зеркало packages/core/api/schema.ts:
// защита границы от дрейфа схемы ответов.
import { type ZodType } from 'zod';

export interface ParseOptions {
  endpoint: string;
}

export function parseWithFallback<T>(
  data: unknown,
  schema: ZodType,
  fallback: T,
  opts: ParseOptions,
): T {
  const result = schema.safeParse(data);
  if (result.success) return result.data as T;
  console.warn(`[api] schema validation failed: ${opts.endpoint}`, {
    issues: result.error.issues,
    received: data,
  });
  return fallback;
}

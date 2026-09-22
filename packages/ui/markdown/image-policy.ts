import { useSyncExternalStore } from 'react';

export type MarkdownExternalImageMode = 'allow' | 'block' | 'allowlist';

export interface MarkdownImagePolicy {
  mode: MarkdownExternalImageMode;
  hosts: ReadonlySet<string>;
}

const DEFAULT_POLICY: MarkdownImagePolicy = { mode: 'allow', hosts: new Set() };

let currentPolicy: MarkdownImagePolicy = DEFAULT_POLICY;
let policyVersion = 0;
const listeners = new Set<() => void>();

function normalizeHost(entry: string): string {
  const trimmed = entry.trim().toLowerCase();
  if (trimmed === '') return '';
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//.test(trimmed) ? trimmed : `https://${trimmed}`;
  try {
    return new URL(withScheme).hostname;
  } catch {
    return '';
  }
}

function sameHosts(a: ReadonlySet<string>, b: ReadonlySet<string>): boolean {
  if (a.size !== b.size) return false;
  for (const host of a) if (!b.has(host)) return false;
  return true;
}

export function setMarkdownImagePolicy(policy: {
  mode: MarkdownExternalImageMode;
  hosts?: readonly string[];
}): void {
  const hosts = new Set<string>();
  for (const entry of policy.hosts ?? []) {
    const host = normalizeHost(entry);
    if (host !== '') hosts.add(host);
  }
  if (policy.mode === currentPolicy.mode && sameHosts(hosts, currentPolicy.hosts)) {
    return;
  }
  currentPolicy = { mode: policy.mode, hosts };
  policyVersion += 1;
  for (const listener of listeners) listener();
}

export function resetMarkdownImagePolicy(): void {
  setMarkdownImagePolicy({ mode: DEFAULT_POLICY.mode, hosts: [] });
}

export function getMarkdownImagePolicy(): MarkdownImagePolicy {
  return currentPolicy;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getVersion(): number {
  return policyVersion;
}

export function useMarkdownImagePolicyVersion(): number {
  return useSyncExternalStore(subscribe, getVersion, getVersion);
}

const SCHEME_RE = /^[a-z][a-z0-9+.-]*:/i;

function stripControlChars(value: string): string {
  let out = '';
  for (const ch of value) {
    if ((ch.codePointAt(0) ?? 0) > 0x20) out += ch;
  }
  return out;
}

export function isTrustedMarkdownImageSrc(src: string): boolean {
  if (typeof src !== 'string' || src === '') return false;
  if (/^data:image\//i.test(src)) return true;

  const cleaned = stripControlChars(src).replace(/\\/g, '/');
  if (cleaned === '') return false;

  const isProtocolRelative = cleaned.startsWith('//');
  if (!isProtocolRelative && !SCHEME_RE.test(cleaned)) {
    return true;
  }
  if (!isProtocolRelative && !/^https?:/i.test(cleaned)) return false;

  if (currentPolicy.mode === 'allow') return true;

  try {
    const url = new URL(isProtocolRelative ? `https:${cleaned}` : cleaned);
    return currentPolicy.hosts.has(url.hostname.toLowerCase());
  } catch {
    return false;
  }
}

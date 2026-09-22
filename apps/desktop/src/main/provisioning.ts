import { execFile } from 'child_process';
import { existsSync, promises as fs, readlinkSync } from 'fs';
import { createHash } from 'crypto';
import { dirname, isAbsolute, join, posix, relative, resolve, sep } from 'path';
import { homedir } from 'os';
import { promisify } from 'util';
import { zstdDecompress as zstdDecompressCb, zstdDecompressSync } from 'zlib';
import { ipcMain } from 'electron';

import { runtimeRoot, type AgentPathContext } from './agent-bootstrap';
import { mcpServersRoot } from './mcp-servers';
import { skillsRoot } from './skills';
import { hashArtifactTree } from '../../scripts/hash-artifact-tree.mjs';
import { mainApiFetch } from './perimeter';
import type {
  ProvisioningPackageStatus,
  ProvisioningPackageType,
  ProvisioningStatus,
  ProvisioningTypeProgress,
  RemovedPackageStatus,
} from '../shared/provisioning-status';
import type { ProvisioningReachability } from '../shared/perimeter-config';

export type PackagePlatform =
  '*' | 'darwin-arm64' | 'darwin-x64' | 'linux-arm64' | 'win-x64' | 'linux-x64';

export type PackageType = 'skill' | 'mcp-server' | 'runtime';

export interface PackageManifest {
  schemaVersion: 1;
  name: string;
  version: string;
  type: PackageType;
  platform: PackagePlatform;
  sha256: string;
  size: number;
  requires: string[];
}

function isPackageType(value: unknown): value is PackageType {
  return value === 'skill' || value === 'mcp-server' || value === 'runtime';
}

function isPackagePlatform(value: unknown): value is PackagePlatform {
  return (
    value === '*' ||
    value === 'darwin-arm64' ||
    value === 'darwin-x64' ||
    value === 'win-x64' ||
    value === 'linux-x64'
  );
}

export function parsePackageManifest(value: unknown): PackageManifest | null {
  if (value === null || typeof value !== 'object') return null;
  const v = value as Record<string, unknown>;

  const requiresRaw = v.requires;
  const requires: string[] | undefined =
    requiresRaw === undefined
      ? []
      : Array.isArray(requiresRaw) && requiresRaw.every((r) => typeof r === 'string')
        ? (requiresRaw as string[])
        : undefined;

  if (
    v.schemaVersion !== 1 ||
    typeof v.name !== 'string' ||
    v.name.length === 0 ||
    typeof v.version !== 'string' ||
    v.version.length === 0 ||
    !isPackageType(v.type) ||
    !isPackagePlatform(v.platform) ||
    typeof v.sha256 !== 'string' ||
    !/^[0-9a-f]{64}$/i.test(v.sha256) ||
    typeof v.size !== 'number' ||
    !Number.isFinite(v.size) ||
    v.size < 0 ||
    requires === undefined
  ) {
    return null;
  }
  return {
    schemaVersion: 1,
    name: v.name,
    version: v.version,
    type: v.type,
    platform: v.platform,
    sha256: v.sha256.toLowerCase(),
    size: v.size,
    requires,
  };
}

export function parseManifestResponse(data: unknown): PackageManifest[] {
  if (data === null || typeof data !== 'object') return [];
  const packages = (data as Record<string, unknown>).packages;
  if (!Array.isArray(packages)) return [];
  const out: PackageManifest[] = [];
  for (const entry of packages) {
    const parsed = parsePackageManifest(entry);
    if (parsed) out.push(parsed);
  }
  return out;
}

export function parseManifestCoverage(data: unknown): {
  totalBeforePlatformFilter: number;
  platformsAvailable: string[];
} {
  if (data === null || typeof data !== 'object') {
    return { totalBeforePlatformFilter: 0, platformsAvailable: [] };
  }
  const rec = data as Record<string, unknown>;
  const total =
    typeof rec.totalBeforePlatformFilter === 'number' &&
    Number.isFinite(rec.totalBeforePlatformFilter) &&
    rec.totalBeforePlatformFilter >= 0
      ? Math.floor(rec.totalBeforePlatformFilter)
      : 0;
  const platforms = Array.isArray(rec.platformsAvailable)
    ? rec.platformsAvailable.filter((p): p is string => typeof p === 'string' && p.length > 0)
    : [];
  return { totalBeforePlatformFilter: total, platformsAvailable: platforms };
}

export interface RevokedPackageRef {
  type: PackageType;
  name: string;
  version?: string;
}

const SAFE_PACKAGE_IDENTIFIER = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/;

function isSafePackageName(name: string): boolean {
  return SAFE_PACKAGE_IDENTIFIER.test(name) && !name.includes('..') && name.length <= 200;
}

export function parseRevokedPackageRef(entry: unknown): RevokedPackageRef | null {
  if (typeof entry !== 'string') return null;
  const colon = entry.indexOf(':');
  if (colon <= 0) return null;
  const type = entry.slice(0, colon);
  if (!isPackageType(type)) return null;
  const rest = entry.slice(colon + 1);
  const at = rest.indexOf('@');
  const name = at >= 0 ? rest.slice(0, at) : rest;
  const version = at >= 0 ? rest.slice(at + 1) : undefined;
  if (!isSafePackageName(name)) return null;
  if (version !== undefined && !isSafePackageName(version)) return null;
  return { type, name, ...(version !== undefined ? { version } : {}) };
}

export function parseRevokedPackages(data: unknown): RevokedPackageRef[] {
  if (data === null || typeof data !== 'object') return [];
  const revoked = (data as Record<string, unknown>).revokedPackages;
  if (!Array.isArray(revoked)) return [];
  const out: RevokedPackageRef[] = [];
  for (const entry of revoked) {
    const parsed = parseRevokedPackageRef(entry);
    if (parsed) out.push(parsed);
  }
  return out;
}

export interface UnavailablePackageRef {
  key: string;
  reason: string;
}

function isUnavailablePackageRef(value: unknown): value is UnavailablePackageRef {
  if (value === null || typeof value !== 'object') return false;
  const v = value as Record<string, unknown>;
  return typeof v.key === 'string' && v.key.length > 0 && typeof v.reason === 'string';
}

export function parseUnavailablePackages(data: unknown): UnavailablePackageRef[] {
  if (data === null || typeof data !== 'object') return [];
  const unavailable = (data as Record<string, unknown>).unavailablePackages;
  if (!Array.isArray(unavailable)) return [];
  const out: UnavailablePackageRef[] = [];
  for (const entry of unavailable) {
    if (isUnavailablePackageRef(entry)) out.push({ key: entry.key, reason: entry.reason });
  }
  return out;
}

export function unavailablePackageName(key: string): string {
  const colon = key.indexOf(':');
  const rest = colon >= 0 ? key.slice(colon + 1) : key;
  const at = rest.indexOf('@');
  return at >= 0 ? rest.slice(0, at) : rest;
}

export const PROVISIONED_MARKER_FILENAME = '.provisioned.json';

export interface ProvisionedMarker {
  name: string;
  version: string;
  platform: string;
  sha256: string;
  installedAt: string;
}

function isProvisionedMarker(value: unknown): value is ProvisionedMarker {
  if (value === null || typeof value !== 'object') return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.name === 'string' &&
    typeof v.version === 'string' &&
    typeof v.platform === 'string' &&
    typeof v.sha256 === 'string' &&
    typeof v.installedAt === 'string'
  );
}

export async function readProvisionedMarker(installDir: string): Promise<ProvisionedMarker | null> {
  try {
    const raw = await fs.readFile(join(installDir, PROVISIONED_MARKER_FILENAME), 'utf-8');
    const parsed: unknown = JSON.parse(raw);
    return isProvisionedMarker(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

async function writeProvisionedMarker(
  installDir: string,
  marker: ProvisionedMarker,
): Promise<void> {
  await fs.writeFile(
    join(installDir, PROVISIONED_MARKER_FILENAME),
    JSON.stringify(marker, null, 2),
    'utf-8',
  );
}

export function packageNeedsSync(
  pkg: Pick<PackageManifest, 'version' | 'sha256'>,
  marker: ProvisionedMarker | null,
): boolean {
  if (!marker) return true;
  return marker.version !== pkg.version || marker.sha256 !== pkg.sha256;
}

export function packageFamilyRoot(ctx: AgentPathContext, type: PackageType): string {
  switch (type) {
    case 'runtime':
      return runtimeRoot(ctx);
    case 'mcp-server':
      return mcpServersRoot(ctx);
    case 'skill':
      return skillsRoot(ctx);
  }
}

export function runtimePackageRoot(ctx: AgentPathContext, name: string): string {
  return join(runtimeRoot(ctx), name);
}

function runtimeVersionDirName(pkg: Pick<PackageManifest, 'name' | 'version'>): string {
  return `${pkg.name}@${pkg.version}`;
}

export function packageInstallDir(
  ctx: AgentPathContext,
  pkg: Pick<PackageManifest, 'type' | 'name' | 'version'>,
): string {
  if (pkg.type === 'runtime') {
    return join(runtimePackageRoot(ctx, pkg.name), runtimeVersionDirName(pkg));
  }
  const root = packageFamilyRoot(ctx, pkg.type);
  return join(root, pkg.name);
}

function runtimeCurrentPath(ctx: AgentPathContext, name: string): string {
  return join(runtimePackageRoot(ctx, name), 'current');
}

async function switchRuntimeCurrent(root: string, versionDirName: string): Promise<void> {
  await fs.mkdir(root, { recursive: true });
  const currentPath = join(root, 'current');
  const tempPath = join(
    root,
    `.current.tmp-${process.pid}-${Date.now()}-${Math.random().toString(36).slice(2)}`,
  );
  await fs.symlink(versionDirName, tempPath);
  try {
    await fs.rename(tempPath, currentPath);
  } catch (err) {
    await fs.rm(tempPath, { force: true }).catch(() => {});
    throw err;
  }
}

function runtimeCurrentTargetName(root: string): string | null {
  try {
    return readlinkSync(join(root, 'current'));
  } catch {
    return null;
  }
}

async function pruneRuntimeVersions(
  root: string,
  packageName: string,
  keep: ReadonlySet<string>,
): Promise<void> {
  let entries;
  try {
    entries = await fs.readdir(root, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    if (!entry.name.startsWith(`${packageName}@`)) continue;
    if (keep.has(entry.name)) continue;
    try {
      await fs.rm(join(root, entry.name), { recursive: true, force: true });
      console.log(`[provisioning] pruned stale runtime version ${entry.name} from ${root}`);
    } catch (err) {
      console.warn(`[provisioning] could not prune stale runtime version ${entry.name}:`, err);
    }
  }
}

async function switchRuntimeCurrentAndPrune(
  root: string,
  packageName: string,
  versionDirName: string,
): Promise<void> {
  const previousDirName = runtimeCurrentTargetName(root);
  await switchRuntimeCurrent(root, versionDirName);
  const keep = new Set([versionDirName]);
  if (previousDirName) keep.add(previousDirName);
  await pruneRuntimeVersions(root, packageName, keep);
}

export function resolveProvisionedRuntimeDir(ctx: AgentPathContext, name: string): string | null {
  const currentPath = runtimeCurrentPath(ctx, name);
  try {
    const target = readlinkSync(currentPath);
    const resolved = join(runtimePackageRoot(ctx, name), target);
    return existsSync(resolved) ? resolved : null;
  } catch {
    return null;
  }
}

export function sha256Hex(buffer: Buffer): string {
  return createHash('sha256').update(buffer).digest('hex');
}

export type TarEntryType = 'file' | 'symlink' | 'directory' | 'other';

export interface TarEntry {
  name: string;
  type: TarEntryType;
  linkname: string;
  data: Buffer;
  mode: number;
}

const TAR_BLOCK_SIZE = 512;

function cstr(field: Buffer): string {
  const nul = field.indexOf(0);
  return (nul >= 0 ? field.subarray(0, nul) : field).toString('utf-8');
}

function parseBase256Field(field: Buffer): number {
  let value = BigInt(field[0] & 0x7f);
  for (let i = 1; i < field.length; i += 1) {
    value = (value << 8n) | BigInt(field[i]);
  }
  const result = Number(value);
  if (!Number.isSafeInteger(result)) {
    throw new Error('tar header numeric field exceeds the safe integer range');
  }
  return result;
}

function parseOctalField(field: Buffer): number {
  if (field.length === 0) return 0;
  if ((field[0] & 0x80) !== 0) {
    return parseBase256Field(field);
  }
  const text = cstr(field).trim();
  if (text.length === 0) return 0;
  if (!/^[0-7]+$/.test(text)) {
    throw new Error(`invalid octal tar header field: ${JSON.stringify(text)}`);
  }
  return parseInt(text, 8);
}

function typeflagKind(flag: string): TarEntryType {
  switch (flag) {
    case '0':
    case '\0':
    case '':
      return 'file';
    case '2':
      return 'symlink';
    case '5':
      return 'directory';
    default:
      return 'other';
  }
}

function parsePaxRecords(data: Buffer): { path?: string; linkpath?: string } {
  const overrides: { path?: string; linkpath?: string } = {};
  let offset = 0;
  while (offset < data.length) {
    const spaceIdx = data.indexOf(0x20, offset);
    if (spaceIdx === -1) break;
    const length = parseInt(data.subarray(offset, spaceIdx).toString('ascii'), 10);
    if (!Number.isFinite(length) || length <= 0 || offset + length > data.length) break;
    const record = data.subarray(offset, offset + length);
    const eqIdx = record.indexOf(0x3d, spaceIdx - offset + 1);
    if (eqIdx !== -1) {
      const key = record.subarray(spaceIdx - offset + 1, eqIdx).toString('utf-8');
      const value = record.subarray(eqIdx + 1, record.length - 1).toString('utf-8');
      if (key === 'path') overrides.path = value;
      if (key === 'linkpath') overrides.linkpath = value;
    }
    offset += length;
  }
  return overrides;
}

export function readTarEntries(buf: Buffer): TarEntry[] {
  const entries: TarEntry[] = [];
  let offset = 0;
  let pendingName: string | null = null;
  let pendingLinkname: string | null = null;

  while (offset + TAR_BLOCK_SIZE <= buf.length) {
    const header = buf.subarray(offset, offset + TAR_BLOCK_SIZE);
    if (header.every((b) => b === 0)) break;

    const rawName = cstr(header.subarray(0, 100));
    const mode = parseOctalField(header.subarray(100, 108));
    const size = parseOctalField(header.subarray(124, 136));
    const typeflagByte = header[156] ?? 0;
    const typeflag = String.fromCharCode(typeflagByte);
    const rawLinkname = cstr(header.subarray(157, 257));
    const prefix = cstr(header.subarray(345, 500));
    const ustarName = prefix.length > 0 ? `${prefix}/${rawName}` : rawName;

    offset += TAR_BLOCK_SIZE;
    const dataBlocks = Math.ceil(size / TAR_BLOCK_SIZE);
    const rawData = buf.subarray(offset, offset + size);

    if (typeflag === 'x' || typeflag === 'g') {
      const overrides = parsePaxRecords(rawData);
      if (overrides.path !== undefined) pendingName = overrides.path;
      if (overrides.linkpath !== undefined) pendingLinkname = overrides.linkpath;
      offset += dataBlocks * TAR_BLOCK_SIZE;
      continue;
    }

    if (typeflag === 'L') {
      pendingName = cstr(rawData);
      offset += dataBlocks * TAR_BLOCK_SIZE;
      continue;
    }

    if (typeflag === 'K') {
      pendingLinkname = cstr(rawData);
      offset += dataBlocks * TAR_BLOCK_SIZE;
      continue;
    }

    const kind = typeflagKind(typeflag);
    const data = kind === 'file' ? Buffer.from(rawData) : Buffer.alloc(0);
    offset += dataBlocks * TAR_BLOCK_SIZE;

    const name = pendingName ?? ustarName;
    const linkname = pendingLinkname ?? rawLinkname;
    pendingName = null;
    pendingLinkname = null;

    entries.push({ name, type: kind, linkname, data, mode });
  }
  return entries;
}

export interface TarSafetyViolation {
  name: string;
  reason: string;
}

function isTraversalFreePath(pathname: string): boolean {
  if (pathname.length === 0) return false;
  if (pathname.startsWith('/') || pathname.startsWith('\\') || /^[A-Za-z]:[\\/]/.test(pathname)) {
    return false;
  }
  const segments = pathname.split(/[/\\]/);
  return !segments.includes('..') && !segments.includes('');
}

function isSymlinkTargetSafe(entryName: string, linkname: string): boolean {
  if (linkname.length === 0) return false;
  if (linkname.startsWith('/') || linkname.startsWith('\\') || /^[A-Za-z]:[\\/]/.test(linkname)) {
    return false;
  }
  const normalizedLinkname = linkname.split(/[\\/]/).join('/');
  const entryDir = posix.dirname(entryName);
  const resolved = posix.normalize(posix.join(entryDir, normalizedLinkname));
  return resolved !== '..' && !resolved.startsWith('../') && !resolved.startsWith('/');
}

export function validateTarEntries(entries: readonly TarEntry[]): TarSafetyViolation[] {
  const violations: TarSafetyViolation[] = [];
  for (const entry of entries) {
    if (!isTraversalFreePath(entry.name)) {
      violations.push({
        name: entry.name,
        reason: 'path escapes the extraction root',
      });
      continue;
    }
    if (entry.type === 'symlink' && !isSymlinkTargetSafe(entry.name, entry.linkname)) {
      violations.push({
        name: entry.name,
        reason: 'symlink target escapes the extraction root',
      });
    }
  }
  return violations;
}

function escapesDestDir(target: string, destDir: string): boolean {
  const rel = relative(resolve(destDir), resolve(target));
  return rel === '..' || rel.startsWith(`..${sep}`) || isAbsolute(rel);
}

const TAR_MODE_PRIVILEGE_BITS_MASK = 0o7000; 
const TAR_MODE_PERMISSION_MASK = 0o777;

function sanitizeTarFileMode(mode: number): number {
  return mode & TAR_MODE_PERMISSION_MASK & ~TAR_MODE_PRIVILEGE_BITS_MASK;
}

async function extractTarEntries(entries: readonly TarEntry[], destDir: string): Promise<void> {
  for (const entry of entries) {
    const target = join(destDir, ...entry.name.split(/[/\\]/));
    if (escapesDestDir(target, destDir)) {
      throw new Error(`entry "${entry.name}" resolved outside the extraction root`);
    }
    if (entry.type === 'directory') {
      await fs.mkdir(target, { recursive: true });
    } else if (entry.type === 'file') {
      await fs.mkdir(dirname(target), { recursive: true });
      await fs.writeFile(target, entry.data);
      await fs.chmod(target, sanitizeTarFileMode(entry.mode));
    } else if (entry.type === 'symlink') {
      await fs.mkdir(dirname(target), { recursive: true });
      await fs.rm(target, { force: true });
      await fs.symlink(entry.linkname, target);
    }
    // "other" (hardlink / char / block / fifo) entries are skipped — not
    // expected in our packages and not worth the extra attack surface.
  }
}

export type ExtractOutcome = { ok: true } | { ok: false; reason: string };

export async function extractPackageTar(
  tarBuffer: Buffer,
  destDir: string,
): Promise<ExtractOutcome> {
  let entries: TarEntry[];
  try {
    entries = readTarEntries(tarBuffer);
  } catch (err) {
    return {
      ok: false,
      reason: `could not parse tar archive: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  const violations = validateTarEntries(entries);
  if (violations.length > 0) {
    return {
      ok: false,
      reason: `rejected unsafe archive entries: ${violations
        .map((v) => `${v.name} (${v.reason})`)
        .join('; ')}`,
    };
  }

  try {
    await fs.mkdir(destDir, { recursive: true });
    await extractTarEntries(entries, destDir);
  } catch (err) {
    return {
      ok: false,
      reason: `could not extract archive: ${err instanceof Error ? err.message : String(err)}`,
    };
  }

  try {
    await hashArtifactTree(destDir);
  } catch (err) {
    return {
      ok: false,
      reason: `extracted tree failed the integrity gate: ${
        err instanceof Error ? err.message : String(err)
      }`,
    };
  }
  return { ok: true };
}

export type ZstdDecompressor = (input: Buffer) => Promise<Buffer>;

const hasBuiltinZstd = typeof zstdDecompressSync === 'function';

const nodeZstdDecompressAsync: ((input: Buffer) => Promise<Buffer>) | null =
  typeof zstdDecompressCb === 'function' ? promisify(zstdDecompressCb) : null;

export function nodeZstdDecompress(input: Buffer): Promise<Buffer> {
  if (!nodeZstdDecompressAsync) {
    return Promise.reject(
      new Error(
        'this Node runtime has no built-in zlib zstd codec (zlib.zstdDecompress) — ' +
          'requires Node 22.15+',
      ),
    );
  }
  return nodeZstdDecompressAsync(input);
}

export function systemZstdDecompress(input: Buffer): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      'zstd',
      ['-d', '-c', '-q'],
      { maxBuffer: 1024 * 1024 * 1024, encoding: 'buffer' },
      (err, stdout: Buffer) => {
        if (err) reject(err);
        else resolve(stdout);
      },
    );
    child.stdin?.end(input);
  });
}

export function defaultZstdDecompress(input: Buffer): Promise<Buffer> {
  if (hasBuiltinZstd) return nodeZstdDecompress(input);
  console.warn(
    '[provisioning] this Node runtime has no built-in zlib zstd codec — ' +
      'falling back to an external `zstd` binary, which will not exist in a ' +
      'closed corporate perimeter and every package install will fail',
  );
  return systemZstdDecompress(input);
}

export type AuthedFetch = (input: string, init?: RequestInit) => Promise<Response>;

export interface ProvisioningAuth {
  apiBaseUrl: string;
  token: string;
  workspaceId: string;
}

type ProvisioningManifestFetchResult =
  | {
      kind: 'ok';
      packages: PackageManifest[];
      revoked: RevokedPackageRef[];
      totalBeforePlatformFilter: number;
      platformsAvailable: string[];
      unavailable: UnavailablePackageRef[];
    }
  | { kind: 'unconfigured' }
  /** The server DEFINITIVELY denies this workspace to this user (issue #242,
   *  T-060: the member was excluded — requireWorkspaceMember answers a
   *  JSON-shaped 403/404). Distinct from "error": transient failures keep
   *  everything installed, an exclusion disarms the machine. */
  | { kind: 'access-revoked' }
  | { kind: 'error' };

async function isAccessDeniedResponse(res: Response): Promise<boolean> {
  if (res.status !== 403 && res.status !== 404) return false;
  try {
    const body: unknown = await res.json();
    return (
      body !== null &&
      typeof body === 'object' &&
      typeof (body as Record<string, unknown>).error === 'string'
    );
  } catch {
    return false;
  }
}

async function fetchProvisioningManifest(
  auth: ProvisioningAuth,
  platform: PackagePlatform,
  fetchImpl: AuthedFetch,
): Promise<ProvisioningManifestFetchResult> {
  const url = `${auth.apiBaseUrl.replace(/\/+$/, '')}/api/provisioning/manifest?platform=${encodeURIComponent(platform)}`;
  try {
    const res = await fetchImpl(url, {
      headers: {
        Authorization: `Bearer ${auth.token}`,
        'X-Workspace-ID': auth.workspaceId,
      },
    });
    if (res.status === 503) {
      return res.headers.get('Retry-After') !== null ? { kind: 'error' } : { kind: 'unconfigured' };
    }
    if (await isAccessDeniedResponse(res)) return { kind: 'access-revoked' };
    if (!res.ok) return { kind: 'error' };
    const data: unknown = await res.json();
    return {
      kind: 'ok',
      packages: parseManifestResponse(data),
      revoked: parseRevokedPackages(data),
      unavailable: parseUnavailablePackages(data),
      ...parseManifestCoverage(data),
    };
  } catch {
    return { kind: 'error' };
  }
}

const PROVISIONING_BLOB_HARD_CEILING_BYTES = 2 * 1024 * 1024 * 1024; 
const PROVISIONING_BLOB_SIZE_TOLERANCE = 1.05; 
const PROVISIONING_BLOB_SIZE_SLACK_BYTES = 1024 * 1024; 

function maxAllowedBlobBytes(manifestSize: number): number {
  const declaredBound =
    Math.ceil(manifestSize * PROVISIONING_BLOB_SIZE_TOLERANCE) + PROVISIONING_BLOB_SIZE_SLACK_BYTES;
  return Math.min(declaredBound, PROVISIONING_BLOB_HARD_CEILING_BYTES);
}

export type DownloadProgressCallback = (bytesDownloaded: number, bytesTotal: number) => void;

const PROVISIONING_DOWNLOAD_MAX_ATTEMPTS = 4;

function downloadTempPathFor(familyRoot: string, pkg: PackageManifest): string {
  return join(familyRoot, `.${pkg.name}@${pkg.version}-${pkg.sha256.slice(0, 12)}.download.tmp`);
}

async function fileSizeIfExists(path: string): Promise<number> {
  try {
    return (await fs.stat(path)).size;
  } catch {
    return 0;
  }
}

async function downloadPackageToFile(
  auth: ProvisioningAuth,
  pkg: PackageManifest,
  fetchImpl: AuthedFetch,
  tempPath: string,
  onProgress?: DownloadProgressCallback,
): Promise<number | null> {
  const url =
    `${auth.apiBaseUrl.replace(/\/+$/, '')}/api/provisioning/blob/` +
    `${encodeURIComponent(pkg.name)}/${encodeURIComponent(pkg.version)}` +
    `?platform=${encodeURIComponent(pkg.platform)}`;
  const maxBytes = maxAllowedBlobBytes(pkg.size);

  for (let attempt = 1; attempt <= PROVISIONING_DOWNLOAD_MAX_ATTEMPTS; attempt += 1) {
    const existingBytes = await fileSizeIfExists(tempPath);
    const requestingRange = existingBytes > 0;
    try {
      const res = await fetchImpl(url, {
        headers: {
          Authorization: `Bearer ${auth.token}`,
          'X-Workspace-ID': auth.workspaceId,
          ...(requestingRange ? { Range: `bytes=${existingBytes}-` } : {}),
        },
      });

      if (!res.ok && res.status !== 206) {
        continue;
      }

      const resuming = requestingRange && res.status === 206;
      if (requestingRange && !resuming) {
        await fs.rm(tempPath, { force: true });
      }
      const baseBytes = resuming ? existingBytes : 0;

      const declaredLength = Number(res.headers.get('content-length'));
      if (Number.isFinite(declaredLength) && baseBytes + declaredLength > maxBytes) {
        console.error(
          `[provisioning] refusing to download ${pkg.name}@${pkg.version}: ` +
            `Content-Length ${declaredLength} exceeds the ${maxBytes}-byte cap`,
        );
        await fs.rm(tempPath, { force: true });
        return null;
      }

      if (!res.body) {
        const buf = Buffer.from(await res.arrayBuffer());
        if (baseBytes + buf.length > maxBytes) {
          console.error(
            `[provisioning] refusing to download ${pkg.name}@${pkg.version}: ` +
              `response exceeded the ${maxBytes}-byte cap`,
          );
          await fs.rm(tempPath, { force: true });
          return null;
        }
        await fs.writeFile(tempPath, buf, { flag: resuming ? 'a' : 'w' });
        const total = baseBytes + buf.length;
        onProgress?.(total, Math.max(pkg.size, total));
        return total;
      }

      const handle = await fs.open(tempPath, resuming ? 'a' : 'w');
      let total = baseBytes;
      try {
        const reader = res.body.getReader();
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          total += value.byteLength;
          if (total > maxBytes) {
            await reader.cancel().catch(() => {});
            console.error(
              `[provisioning] refusing to download ${pkg.name}@${pkg.version}: ` +
                `response exceeded the ${maxBytes}-byte cap`,
            );
            await handle.close();
            await fs.rm(tempPath, { force: true });
            return null;
          }
          await handle.write(value);
          onProgress?.(total, Math.max(pkg.size, total));
        }
      } finally {
        await handle.close();
      }
      return total;
    } catch {
      continue;
    }
  }
  return null;
}

export type DaemonBusyCheck = () => Promise<boolean>;

async function preserveUnmarkedLegacyContent(installDir: string): Promise<string> {
  const base = `${installDir}.pre-0.7.0`;
  const target = existsSync(base) ? `${base}-${process.pid}-${Date.now()}` : base;
  await fs.rename(installDir, target);
  console.warn(
    `[provisioning] preserved unmarked pre-0.7.0 content at ${target} before installing the provisioned package into ${installDir}`,
  );
  return target;
}

export async function atomicSwapInstall(
  stagingDir: string,
  installDir: string,
  marker: ProvisionedMarker,
  isDaemonBusy?: DaemonBusyCheck,
  onLegacyContentPreserved?: (path: string) => void,
): Promise<'installed' | 'skipped' | 'deferred'> {
  if (!existsSync(installDir)) {
    try {
      await fs.rename(stagingDir, installDir);
      return 'installed';
    } catch (err) {
      if (!existsSync(installDir)) throw err;
      // Someone else created installDir between our check and the rename —
      // fall through to the reconciliation path below.
    }
  }

  const current = await readProvisionedMarker(installDir);
  if (current && current.version === marker.version && current.sha256 === marker.sha256) {
    await fs.rm(stagingDir, { recursive: true, force: true }).catch(() => {});
    return 'skipped';
  }

  if (isDaemonBusy && (await isDaemonBusy())) {
    return 'deferred';
  }

  if (!current) {
    const preservedPath = await preserveUnmarkedLegacyContent(installDir);
    onLegacyContentPreserved?.(preservedPath);
    await fs.rename(stagingDir, installDir);
    return 'installed';
  }

  const displaced = `${installDir}.displaced-${process.pid}-${Date.now()}`;
  await fs.rename(installDir, displaced);
  try {
    await fs.rename(stagingDir, installDir);
  } catch (err) {
    await fs.rename(displaced, installDir).catch(() => {});
    throw err;
  }
  await fs.rm(displaced, { recursive: true, force: true }).catch(() => {});
  return 'installed';
}

function stagingDirFor(familyRoot: string, name: string): string {
  return join(
    familyRoot,
    `.${name}.staging-${process.pid}-${Date.now()}-${Math.random().toString(36).slice(2)}`,
  );
}

export type RevokedRemovalOutcome = 'removed' | 'absent' | 'unmarked-kept' | 'deferred' | 'fail';

async function removeRevokedRuntimePackage(
  root: string,
  name: string,
): Promise<RevokedRemovalOutcome> {
  let entries;
  try {
    entries = await fs.readdir(root, { withFileTypes: true });
  } catch {
    return 'absent';
  }
  const marked: string[] = [];
  let unmarkedVersions = 0;
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    if (!entry.name.startsWith(`${name}@`)) continue;
    if (await readProvisionedMarker(join(root, entry.name))) {
      marked.push(entry.name);
    } else {
      unmarkedVersions += 1;
    }
  }
  if (marked.length === 0) {
    return unmarkedVersions > 0 ? 'unmarked-kept' : 'absent';
  }

  let failed = false;
  for (const dirName of marked) {
    try {
      await fs.rm(join(root, dirName), { recursive: true, force: true });
    } catch (err) {
      console.warn(`[provisioning] could not remove revoked runtime version ${dirName}:`, err);
      failed = true;
    }
  }

  const currentTarget = runtimeCurrentTargetName(root);
  if (currentTarget !== null && !existsSync(join(root, currentTarget))) {
    await fs.rm(join(root, 'current'), { force: true }).catch(() => {});
  }
  await fs.rmdir(root).catch(() => {});
  return failed ? 'fail' : 'removed';
}

export async function removeOneRevokedPackage(
  ctx: AgentPathContext,
  ref: RevokedPackageRef,
  isDaemonBusy?: DaemonBusyCheck,
): Promise<RevokedRemovalOutcome> {
  if (!isSafePackageName(ref.name)) return 'fail';

  try {
    if (ref.type === 'runtime') {
      const root = runtimePackageRoot(ctx, ref.name);
      if (!existsSync(root)) return 'absent';
      if (isDaemonBusy && (await isDaemonBusy())) return 'deferred';
      return await removeRevokedRuntimePackage(root, ref.name);
    }

    const installDir = join(packageFamilyRoot(ctx, ref.type), ref.name);
    if (!existsSync(installDir)) return 'absent';
    const marker = await readProvisionedMarker(installDir);
    if (!marker) {
      return 'unmarked-kept';
    }
    if (isDaemonBusy && (await isDaemonBusy())) return 'deferred';
    await fs.rm(installDir, { recursive: true, force: true });
    return 'removed';
  } catch (err) {
    console.warn(`[provisioning] could not remove revoked package ${ref.type}:${ref.name}:`, err);
    return 'fail';
  }
}

export async function listInstalledProvisionedPackages(
  ctx: AgentPathContext,
): Promise<RevokedPackageRef[]> {
  const out: RevokedPackageRef[] = [];

  for (const type of ['skill', 'mcp-server'] as const) {
    const root = packageFamilyRoot(ctx, type);
    let entries;
    try {
      entries = await fs.readdir(root, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      if (await readProvisionedMarker(join(root, entry.name))) {
        out.push({ type, name: entry.name });
      }
    }
  }

  const rtRoot = runtimeRoot(ctx);
  let rtEntries;
  try {
    rtEntries = await fs.readdir(rtRoot, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const entry of rtEntries) {
    if (!entry.isDirectory()) continue;
    const pkgRoot = join(rtRoot, entry.name);
    let versions;
    try {
      versions = await fs.readdir(pkgRoot, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const version of versions) {
      if (!version.isDirectory()) continue;
      if (!version.name.startsWith(`${entry.name}@`)) continue;
      if (await readProvisionedMarker(join(pkgRoot, version.name))) {
        out.push({ type: 'runtime', name: entry.name });
        break;
      }
    }
  }
  return out;
}

export interface SyncOneDeps {
  fetchImpl: AuthedFetch;
  decompress: ZstdDecompressor;
  isDaemonBusy?: DaemonBusyCheck;
  onInstalled?: (pkg: PackageManifest) => void;
  onProgress?: DownloadProgressCallback;
  onReasonCode?: (reasonCode: string) => void;
  onLegacyContentPreserved?: (path: string) => void;
  localBlobPath?: string;
}

export async function syncOnePackage(
  ctx: AgentPathContext,
  pkg: PackageManifest,
  auth: ProvisioningAuth,
  deps: SyncOneDeps,
): Promise<ProvisioningPackageStatus['state']> {
  const installDir = packageInstallDir(ctx, pkg);
  const marker = await readProvisionedMarker(installDir);
  if (!packageNeedsSync(pkg, marker)) {
    if (pkg.type === 'runtime' && resolveProvisionedRuntimeDir(ctx, pkg.name) !== installDir) {
      try {
        await switchRuntimeCurrentAndPrune(
          runtimePackageRoot(ctx, pkg.name),
          pkg.name,
          runtimeVersionDirName(pkg),
        );
      } catch (err) {
        console.error(
          `[provisioning] could not switch ${pkg.name} current to already-installed ${pkg.version}:`,
          err,
        );
        return 'fail';
      }
    }
    return 'ok';
  }

  const familyRoot =
    pkg.type === 'runtime' ? runtimePackageRoot(ctx, pkg.name) : packageFamilyRoot(ctx, pkg.type);
  try {
    await fs.mkdir(familyRoot, { recursive: true });
  } catch (err) {
    console.error(
      `[provisioning] could not create ${familyRoot} for ${pkg.name}@${pkg.version}:`,
      err,
    );
    return 'fail';
  }
  const stagingDir = stagingDirFor(familyRoot, pkg.name);
  const cleanupStaging = () => fs.rm(stagingDir, { recursive: true, force: true }).catch(() => {});
  const tempDownloadPath = downloadTempPathFor(familyRoot, pkg);

  let downloadedBytes: number | null;
  if (deps.localBlobPath !== undefined) {
    try {
      await fs.copyFile(deps.localBlobPath, tempDownloadPath);
      downloadedBytes = (await fs.stat(tempDownloadPath)).size;
      deps.onProgress?.(downloadedBytes, pkg.size);
    } catch (err) {
      console.error(
        `[provisioning] could not read baked-in blob ${deps.localBlobPath} for ${pkg.name}@${pkg.version}:`,
        err,
      );
      downloadedBytes = null;
    }
  } else {
    downloadedBytes = await downloadPackageToFile(
      auth,
      pkg,
      deps.fetchImpl,
      tempDownloadPath,
      deps.onProgress,
    );
  }
  if (downloadedBytes === null) {
    console.warn(
      `[provisioning] could not download ${pkg.name}@${pkg.version} after ` +
        `${PROVISIONING_DOWNLOAD_MAX_ATTEMPTS} attempt(s) — keeping partial ` +
        `bytes on disk for the next sync attempt`,
    );
    deps.onReasonCode?.('provisioning_download_failed');
    return 'fail';
  }

  let blob: Buffer;
  try {
    blob = await fs.readFile(tempDownloadPath);
  } catch (err) {
    console.error(
      `[provisioning] could not read the downloaded blob for ${pkg.name}@${pkg.version}:`,
      err,
    );
    deps.onReasonCode?.('provisioning_download_failed');
    return 'fail';
  }

  const actualSha256 = sha256Hex(blob);
  if (actualSha256 !== pkg.sha256) {
    console.error(
      `[provisioning] refusing to install ${pkg.name}@${pkg.version}: sha256 mismatch ` +
        `(manifest ${pkg.sha256.slice(0, 12)}…, download ${actualSha256.slice(0, 12)}…)`,
    );
    await fs.rm(tempDownloadPath, { force: true }).catch(() => {});
    await cleanupStaging();
    deps.onReasonCode?.('provisioning_sha256_mismatch');
    return 'fail';
  }
  await fs.rm(tempDownloadPath, { force: true }).catch(() => {});

  let tarBuffer: Buffer;
  try {
    tarBuffer = await deps.decompress(blob);
  } catch (err) {
    console.error(`[provisioning] could not decompress ${pkg.name}@${pkg.version}:`, err);
    await cleanupStaging();
    return 'fail';
  }

  const extracted = await extractPackageTar(tarBuffer, stagingDir);
  if (!extracted.ok) {
    console.error(`[provisioning] ${pkg.name}@${pkg.version}: ${extracted.reason}`);
    await cleanupStaging();
    return 'fail';
  }

  const nextMarker: ProvisionedMarker = {
    name: pkg.name,
    version: pkg.version,
    platform: pkg.platform,
    sha256: pkg.sha256,
    installedAt: new Date().toISOString(),
  };

  try {
    await writeProvisionedMarker(stagingDir, nextMarker);
    const outcome = await atomicSwapInstall(
      stagingDir,
      installDir,
      nextMarker,
      deps.isDaemonBusy,
      deps.onLegacyContentPreserved,
    );
    if (outcome === 'deferred') {
      console.log(
        `[provisioning] deferring install of ${pkg.name}@${pkg.version}: ` +
          `the daemon has active tasks — will retry on the next sync`,
      );
      await cleanupStaging();
      return 'pending';
    }
    if (outcome === 'installed') {
      console.log(`[provisioning] installed ${pkg.name}@${pkg.version} → ${installDir}`);
      deps.onInstalled?.(pkg);
    }
    if (pkg.type === 'runtime') {
      await switchRuntimeCurrentAndPrune(
        runtimePackageRoot(ctx, pkg.name),
        pkg.name,
        runtimeVersionDirName(pkg),
      );
    }
    return 'ok';
  } catch (err) {
    console.error(`[provisioning] could not install ${pkg.name}@${pkg.version}:`, err);
    await cleanupStaging();
    return 'fail';
  }
}

export function currentPackagePlatform(platform: NodeJS.Platform, arch: string): PackagePlatform {
  if (platform === 'darwin') return arch === 'arm64' ? 'darwin-arm64' : 'darwin-x64';
  if (platform === 'win32') return 'win-x64';
  return arch === 'arm64' ? 'linux-arm64' : 'linux-x64';
}

interface ProvisioningAuthState {
  apiBaseUrl: string | null;
  token: string | null;
  workspaceId: string | null;
}

let authState: ProvisioningAuthState = {
  apiBaseUrl: null,
  token: null,
  workspaceId: null,
};

export function setProvisioningCredentials(apiBaseUrl: string | null, token: string | null): void {
  if (apiBaseUrl !== authState.apiBaseUrl || token !== authState.token) {
    resetAccessRevokedStrikes();
  }
  authState = { ...authState, apiBaseUrl, token };
}

export function setProvisioningWorkspaceId(workspaceId: string | null): void {
  if (workspaceId !== authState.workspaceId) {
    resetAccessRevokedStrikes();
  }
  authState = { ...authState, workspaceId };
}

export function resolvedProvisioningAuth(): ProvisioningAuth | null {
  return resolvedAuth();
}

export function provisioningAuthGap(): 'no auth' | 'no workspace' | null {
  const { apiBaseUrl, token, workspaceId } = authState;
  if (!apiBaseUrl || !token) return 'no auth';
  if (!workspaceId) return 'no workspace';
  return null;
}

function resolvedAuth(): ProvisioningAuth | null {
  const { apiBaseUrl, token, workspaceId } = authState;
  if (!apiBaseUrl || !token || !workspaceId) return null;
  return { apiBaseUrl, token, workspaceId };
}

const ZERO_TYPE_PROGRESS: ProvisioningTypeProgress = {
  installed: 0,
  total: 0,
  failed: 0,
};

let restartPendingState = false;
let preservedLegacyPathsState: string[] = [];
let unavailablePackagesState: UnavailablePackageRef[] = [];

function restartAndLegacyFields(): Pick<
  ProvisioningStatus,
  'restartPending' | 'preservedLegacyPaths' | 'removedPackages' | 'unavailablePackages'
> {
  return {
    restartPending: restartPendingState,
    preservedLegacyPaths: [...preservedLegacyPathsState],
    removedPackages: removedPackagesState.map((entry) => ({ ...entry })),
    unavailablePackages: unavailablePackagesState.map((entry) => ({ ...entry })),
  };
}

function recordUnavailablePackages(list: UnavailablePackageRef[]): void {
  unavailablePackagesState = [...list];
  lastStatus = { ...lastStatus, unavailablePackages: [...unavailablePackagesState] };
}

export function setProvisioningRestartPending(pending: boolean): void {
  restartPendingState = pending;
  lastStatus = { ...lastStatus, restartPending: pending };
}

function recordPreservedLegacyPath(path: string): void {
  if (!preservedLegacyPathsState.includes(path)) {
    preservedLegacyPathsState = [...preservedLegacyPathsState, path];
  }
  lastStatus = {
    ...lastStatus,
    preservedLegacyPaths: [...preservedLegacyPathsState],
  };
}

let removedPackagesState: RemovedPackageStatus[] = [];

function recordRemovedPackage(ref: RevokedPackageRef, state: RemovedPackageStatus['state']): void {
  const entry: RemovedPackageStatus = {
    name: ref.name,
    type: ref.type,
    ...(ref.version !== undefined ? { version: ref.version } : {}),
    state,
  };
  const rest = removedPackagesState.filter(
    (existing) => existing.type !== ref.type || existing.name !== ref.name,
  );
  removedPackagesState = [...rest, entry];
  lastStatus = { ...lastStatus, removedPackages: removedPackagesState.map((e) => ({ ...e })) };
}

function clearRemovedPackage(type: string, name: string): void {
  const next = removedPackagesState.filter(
    (existing) => existing.type !== type || existing.name !== name,
  );
  if (next.length === removedPackagesState.length) return;
  removedPackagesState = next;
  lastStatus = { ...lastStatus, removedPackages: removedPackagesState.map((e) => ({ ...e })) };
}

function idleProvisioningStatus(): ProvisioningStatus {
  return {
    configured: true,
    state: 'idle',
    byType: {
      skill: { ...ZERO_TYPE_PROGRESS },
      'mcp-server': { ...ZERO_TYPE_PROGRESS },
      runtime: { ...ZERO_TYPE_PROGRESS },
    },
    summary: { ...ZERO_TYPE_PROGRESS },
    packages: [],
    ...restartAndLegacyFields(),
  };
}

let lastStatus: ProvisioningStatus = idleProvisioningStatus();

let restartHook: (() => void) | null = null;

export function setProvisioningRestartHook(hook: (() => void) | null): void {
  restartHook = hook;
}

let daemonBusyHook: DaemonBusyCheck | null = null;

export function setProvisioningDaemonBusyHook(hook: DaemonBusyCheck | null): void {
  daemonBusyHook = hook;
}

export function getProvisioningStatus(): ProvisioningStatus {
  return lastStatus;
}

export function resetProvisioningStatusForTests(): void {
  restartPendingState = false;
  preservedLegacyPathsState = [];
  removedPackagesState = [];
  lastStatus = idleProvisioningStatus();
  syncInFlight = null;
  resetAccessRevokedStrikes();
}

export interface BakedStorePackage {
  pkg: PackageManifest;
  blobPath: string;
}

export function defaultBakedStoreDir(): string | null {
  const resourcesPath = process.resourcesPath;
  if (!resourcesPath) return null;
  const dir = join(resourcesPath, 'provisioning-packages');
  return existsSync(dir) ? dir : null;
}

export async function readBakedStoreManifest(
  storeDir: string,
  platform: PackagePlatform,
): Promise<BakedStorePackage[]> {
  let entries: string[];
  try {
    entries = await fs.readdir(storeDir);
  } catch {
    return [];
  }
  const out: BakedStorePackage[] = [];
  const seen = new Set<string>();
  for (const entry of entries.filter((e) => e.endsWith('.manifest.json')).sort()) {
    const manifestPath = join(storeDir, entry);
    let parsed: PackageManifest | null = null;
    try {
      parsed = parsePackageManifest(JSON.parse(await fs.readFile(manifestPath, 'utf-8')));
    } catch {
      parsed = null;
    }
    if (!parsed) {
      console.warn(`[provisioning] baked-in store: skipping malformed ${entry}`);
      continue;
    }
    if (parsed.platform !== '*' && parsed.platform !== platform) {
      console.warn(
        `[provisioning] baked-in store: ${entry} is for ${parsed.platform}, this machine is ${platform} — skipping`,
      );
      continue;
    }
    const key = `${parsed.type}:${parsed.name}`;
    if (seen.has(key)) {
      console.warn(`[provisioning] baked-in store: duplicate ${key} in ${entry} — skipping`);
      continue;
    }
    const blobPath = join(storeDir, `${entry.slice(0, -'.manifest.json'.length)}.tar.zst`);
    if (!existsSync(blobPath)) {
      console.warn(`[provisioning] baked-in store: ${entry} has no blob beside it — skipping`);
      continue;
    }
    seen.add(key);
    out.push({ pkg: parsed, blobPath });
  }
  return out;
}

export interface ProvisioningSyncOptions {
  ctx: AgentPathContext;
  bakedStoreDir?: string | null;
  fetchImpl?: AuthedFetch;
  decompress?: ZstdDecompressor;
  isDaemonBusy?: DaemonBusyCheck;
  platform?: NodeJS.Platform;
  arch?: string;
  now?: () => number;
}

function typeProgress(
  statuses: readonly ProvisioningPackageStatus[],
  type: ProvisioningPackageType,
): ProvisioningTypeProgress {
  const ofType = statuses.filter((s) => s.type === type);
  return {
    installed: ofType.filter((s) => s.state === 'ok').length,
    total: ofType.length,
    failed: ofType.filter((s) => s.state === 'fail').length,
  };
}

function inProgressProvisioningStatus(
  statuses: readonly ProvisioningPackageStatus[],
): ProvisioningStatus {
  return {
    configured: true,
    state: 'syncing',
    byType: {
      skill: typeProgress(statuses, 'skill'),
      'mcp-server': typeProgress(statuses, 'mcp-server'),
      runtime: typeProgress(statuses, 'runtime'),
    },
    summary: {
      installed: statuses.filter((s) => s.state === 'ok').length,
      total: statuses.length,
      failed: statuses.filter((s) => s.state === 'fail').length,
    },
    packages: [...statuses],
    ...restartAndLegacyFields(),
  };
}

function completedProvisioningStatus(statuses: ProvisioningPackageStatus[]): ProvisioningStatus {
  const byType = {
    skill: typeProgress(statuses, 'skill'),
    'mcp-server': typeProgress(statuses, 'mcp-server'),
    runtime: typeProgress(statuses, 'runtime'),
  };
  const summary: ProvisioningTypeProgress = {
    installed: statuses.filter((s) => s.state === 'ok').length,
    total: statuses.length,
    failed: statuses.filter((s) => s.state === 'fail').length,
  };
  const anyFailed = summary.failed > 0;
  return {
    configured: true,
    state: anyFailed ? 'fail' : 'ok',
    byType,
    summary,
    reasonCode: anyFailed ? 'provisioning_package_failed' : undefined,
    packages: statuses,
    ...restartAndLegacyFields(),
  };
}

function notConfiguredProvisioningStatus(): ProvisioningStatus {
  return {
    configured: false,
    state: 'ok',
    byType: {
      skill: { ...ZERO_TYPE_PROGRESS },
      'mcp-server': { ...ZERO_TYPE_PROGRESS },
      runtime: { ...ZERO_TYPE_PROGRESS },
    },
    summary: { ...ZERO_TYPE_PROGRESS },
    reasonCode: 'provisioning_not_configured',
    packages: [],
    ...restartAndLegacyFields(),
  };
}

function platformUncoveredProvisioningStatus(
  platform: PackagePlatform,
  platformsAvailable: string[],
  total: number,
): ProvisioningStatus {
  return {
    configured: true,
    state: 'ok',
    byType: {
      skill: { ...ZERO_TYPE_PROGRESS },
      'mcp-server': { ...ZERO_TYPE_PROGRESS },
      runtime: { ...ZERO_TYPE_PROGRESS },
    },
    summary: { ...ZERO_TYPE_PROGRESS },
    reasonCode: 'provisioning_platform_uncovered',
    packages: [],
    platform,
    platformsAvailable,
    totalBeforePlatformFilter: total,
    ...restartAndLegacyFields(),
  };
}

const ACCESS_REVOKED_CONFIRMATIONS_REQUIRED = 3;
const ACCESS_REVOKED_MIN_WINDOW_MS = 10 * 60_000;

let accessRevokedStrikeCount = 0;
let accessRevokedFirstStrikeAtMs: number | null = null;

function recordAccessRevokedStrike(nowMs: number): boolean {
  if (accessRevokedStrikeCount === 0 || accessRevokedFirstStrikeAtMs === null) {
    accessRevokedFirstStrikeAtMs = nowMs;
  }
  accessRevokedStrikeCount += 1;
  return (
    accessRevokedStrikeCount >= ACCESS_REVOKED_CONFIRMATIONS_REQUIRED &&
    nowMs - accessRevokedFirstStrikeAtMs >= ACCESS_REVOKED_MIN_WINDOW_MS
  );
}

function resetAccessRevokedStrikes(): void {
  accessRevokedStrikeCount = 0;
  accessRevokedFirstStrikeAtMs = null;
}

function accessRevokedProvisioningStatus(): ProvisioningStatus {
  return {
    configured: true,
    state: 'fail',
    byType: {
      skill: { ...ZERO_TYPE_PROGRESS },
      'mcp-server': { ...ZERO_TYPE_PROGRESS },
      runtime: { ...ZERO_TYPE_PROGRESS },
    },
    summary: { ...ZERO_TYPE_PROGRESS },
    reasonCode: 'provisioning_access_revoked',
    packages: [],
    ...restartAndLegacyFields(),
  };
}

let lastSkipSignature: string | null = null;

async function runSync(options: ProvisioningSyncOptions): Promise<void> {
  const auth = resolvedAuth();
  if (!auth) {
    const missing = [
      authState.apiBaseUrl ? null : 'apiBaseUrl',
      authState.token ? null : 'token',
      authState.workspaceId ? null : 'workspaceId',
    ].filter((name): name is string => name !== null);
    const signature = missing.join(',');
    if (signature !== lastSkipSignature) {
      lastSkipSignature = signature;
      console.warn(
        `[provisioning] sync skipped — no ${signature}. Packages will not be ` +
          `delivered until this is resolved.`,
      );
    }
    return;
  }
  lastSkipSignature = null;

  lastStatus = { ...lastStatus, state: 'syncing' };

  const fetchImpl = options.fetchImpl ?? mainApiFetch;
  const decompress = options.decompress ?? defaultZstdDecompress;
  const isDaemonBusy = options.isDaemonBusy ?? daemonBusyHook ?? undefined;
  const platform = currentPackagePlatform(
    options.platform ?? process.platform,
    options.arch ?? process.arch,
  );

  const manifestResult = await fetchProvisioningManifest(auth, platform, fetchImpl);

  if (manifestResult.kind !== 'access-revoked') {
    resetAccessRevokedStrikes();
  }

  const bakedStoreDir =
    options.bakedStoreDir !== undefined ? options.bakedStoreDir : defaultBakedStoreDir();
  const readBakedStore = async (): Promise<BakedStorePackage[]> =>
    bakedStoreDir ? readBakedStoreManifest(bakedStoreDir, platform) : [];

  if (manifestResult.kind === 'unconfigured') {
    recordUnavailablePackages([]);
    const baked = await readBakedStore();
    if (baked.length === 0) {
      lastStatus = notConfiguredProvisioningStatus();
      return;
    }
    await installManifestPass(
      options,
      auth,
      baked.map((b) => b.pkg),
      [],
      {
        fetchImpl,
        decompress,
        isDaemonBusy,
        localBlobs: new Map(baked.map((b) => [`${b.pkg.type}:${b.pkg.name}`, b.blobPath])),
      },
    );
    return;
  }
  if (manifestResult.kind === 'access-revoked') {
    if (!recordAccessRevokedStrike((options.now ?? Date.now)())) {
      lastStatus = {
        ...lastStatus,
        state: 'fail',
        reasonCode: 'provisioning_access_revoked',
      };
      return;
    }
    const installed = await listInstalledProvisionedPackages(options.ctx);
    let removedAny = false;
    for (const ref of installed) {
      const outcome = await removeOneRevokedPackage(options.ctx, ref, isDaemonBusy);
      if (outcome === 'removed') {
        removedAny = true;
        recordRemovedPackage(ref, 'removed');
      } else if (outcome === 'deferred' || outcome === 'fail') {
        recordRemovedPackage(ref, 'pending');
      }
    }
    lastStatus = accessRevokedProvisioningStatus();
    if (removedAny) restartHook?.();
    return;
  }
  if (manifestResult.kind === 'error') {
    console.warn('[provisioning] manifest fetch failed — keeping the last known package counts');
    lastStatus = {
      ...lastStatus,
      state: 'fail',
      reasonCode: 'provisioning_manifest_unreachable',
    };
    return;
  }

  recordUnavailablePackages(manifestResult.unavailable);

  const manifest = manifestResult.packages;
  if (manifest.length === 0) {
    const removedAny = await processRevokedPackages(
      options.ctx,
      manifestResult.revoked,
      manifest,
      isDaemonBusy,
    );
    if (removedAny) restartHook?.();
    const revokedKeys = new Set(manifestResult.revoked.map((r) => `${r.type}:${r.name}`));
    const baked = (await readBakedStore()).filter(
      (b) => !revokedKeys.has(`${b.pkg.type}:${b.pkg.name}`),
    );
    if (baked.length === 0) {
      lastStatus =
        manifestResult.totalBeforePlatformFilter > 0
          ? platformUncoveredProvisioningStatus(
              platform,
              manifestResult.platformsAvailable,
              manifestResult.totalBeforePlatformFilter,
            )
          : notConfiguredProvisioningStatus();
      return;
    }
    await installManifestPass(
      options,
      auth,
      baked.map((b) => b.pkg),
      [],
      {
        fetchImpl,
        decompress,
        isDaemonBusy,
        localBlobs: new Map(baked.map((b) => [`${b.pkg.type}:${b.pkg.name}`, b.blobPath])),
      },
    );
    return;
  }

  await installManifestPass(options, auth, manifest, manifestResult.revoked, {
    fetchImpl,
    decompress,
    isDaemonBusy,
    localBlobs: null,
  });
}

async function installManifestPass(
  options: ProvisioningSyncOptions,
  auth: ProvisioningAuth,
  manifest: PackageManifest[],
  revoked: RevokedPackageRef[],
  deps: {
    fetchImpl: AuthedFetch;
    decompress: ZstdDecompressor;
    isDaemonBusy?: DaemonBusyCheck;
    localBlobs: Map<string, string> | null;
  },
): Promise<void> {
  const { fetchImpl, decompress, isDaemonBusy, localBlobs } = deps;
  let installedAnyThisPass = false;
  const statuses: ProvisioningPackageStatus[] = manifest.map((pkg) => ({
    name: pkg.name,
    type: pkg.type,
    version: pkg.version,
    state: 'pending',
  }));
  lastStatus = inProgressProvisioningStatus(statuses);

  for (let i = 0; i < manifest.length; i += 1) {
    const pkg = manifest[i];
    let reasonCode: string | undefined;
    const state = await syncOnePackage(options.ctx, pkg, auth, {
      fetchImpl,
      decompress,
      isDaemonBusy,
      localBlobPath: localBlobs?.get(`${pkg.type}:${pkg.name}`),
      onInstalled: () => {
        installedAnyThisPass = true;
      },
      onProgress: (bytesDownloaded, bytesTotal) => {
        statuses[i] = { ...statuses[i], bytesDownloaded, bytesTotal };
        lastStatus = inProgressProvisioningStatus(statuses);
      },
      onReasonCode: (code) => {
        reasonCode = code;
      },
      onLegacyContentPreserved: recordPreservedLegacyPath,
    });
    statuses[i] = {
      ...statuses[i],
      state,
      ...(reasonCode ? { reasonCode } : {}),
    };
    if (state === 'ok') {
      clearRemovedPackage(pkg.type, pkg.name);
    }
    lastStatus = inProgressProvisioningStatus(statuses);
  }

  const removedAnyThisPass = await processRevokedPackages(
    options.ctx,
    revoked,
    manifest,
    isDaemonBusy,
  );

  lastStatus = completedProvisioningStatus(statuses);

  if (installedAnyThisPass || removedAnyThisPass) {
    restartHook?.();
  }
}

async function processRevokedPackages(
  ctx: AgentPathContext,
  revoked: RevokedPackageRef[],
  manifest: readonly PackageManifest[],
  isDaemonBusy?: DaemonBusyCheck,
): Promise<boolean> {
  if (revoked.length === 0) return false;
  const served = new Set(manifest.map((pkg) => `${pkg.type}:${pkg.name}`));
  let removedAny = false;
  for (const ref of revoked) {
    if (served.has(`${ref.type}:${ref.name}`)) continue;
    const outcome = await removeOneRevokedPackage(ctx, ref, isDaemonBusy);
    switch (outcome) {
      case 'removed':
        removedAny = true;
        recordRemovedPackage(ref, 'removed');
        console.log(`[provisioning] removed revoked package ${ref.type}:${ref.name}`);
        break;
      case 'deferred':
      case 'fail':
        recordRemovedPackage(ref, 'pending');
        break;
      case 'absent':
      case 'unmarked-kept':
        break;
    }
  }
  return removedAny;
}

let syncInFlight: Promise<void> | null = null;

function runTrackedSync(pending: Promise<void>): Promise<void> {
  syncInFlight = pending;
  void pending.finally(() => {
    if (syncInFlight === pending) syncInFlight = null;
  });
  return pending;
}

export function syncProvisionedPackages(options: ProvisioningSyncOptions): Promise<void> {
  if (syncInFlight) return syncInFlight;
  return runTrackedSync(runSync(options));
}

export function retryProvisioning(
  options: ProvisioningSyncOptions = { ctx: pathCtx() },
): Promise<void> {
  return runTrackedSync(runSync(options));
}

export async function runWorkspaceAccessRevokedCleanup(
  workspaceId: string | null,
  options: ProvisioningSyncOptions = { ctx: pathCtx() },
): Promise<void> {
  if (!workspaceId || authState.workspaceId !== workspaceId) return;
  const isDaemonBusy = options.isDaemonBusy ?? daemonBusyHook ?? undefined;
  const installed = await listInstalledProvisionedPackages(options.ctx);
  let removedAny = false;
  for (const ref of installed) {
    const outcome = await removeOneRevokedPackage(options.ctx, ref, isDaemonBusy);
    if (outcome === 'removed') {
      removedAny = true;
      recordRemovedPackage(ref, 'removed');
    } else if (outcome === 'deferred' || outcome === 'fail') {
      recordRemovedPackage(ref, 'pending');
    }
  }
  lastStatus = accessRevokedProvisioningStatus();
  if (removedAny) restartHook?.();
}

export function bindProvisioningWorkspace(
  workspaceId: string | null,
  options: ProvisioningSyncOptions = { ctx: pathCtx() },
): Promise<void> {
  setProvisioningWorkspaceId(workspaceId);
  return syncProvisionedPackages(options).catch((err) => {
    console.warn('[provisioning] sync-on-workspace failed:', err);
  });
}

export function bindProvisioningCredentials(
  apiBaseUrl: string | null,
  token: string | null,
  options: ProvisioningSyncOptions = { ctx: pathCtx() },
): Promise<void> {
  setProvisioningCredentials(apiBaseUrl, token);
  return syncProvisionedPackages(options).catch((err) => {
    console.warn('[provisioning] sync-on-credentials failed:', err);
  });
}

export async function probeProvisioningReachability(): Promise<ProvisioningReachability> {
  if (!resolvedAuth()) return { kind: 'skipped' };
  const status = lastStatus;
  if (status.state === 'idle') return { kind: 'unknown' };
  if (!status.configured) {
    const removed = status.removedPackages ?? [];
    return {
      kind: 'unconfigured',
      ...(removed.length > 0 ? { removedPackages: removed.map((entry) => ({ ...entry })) } : {}),
    };
  }
  return {
    kind: 'status',
    state: status.state,
    installed: status.summary.installed,
    total: status.summary.total,
    failed: status.summary.failed,
    restartPending: status.restartPending === true,
    preservedLegacyPaths: status.preservedLegacyPaths ?? [],
    removedPackages: (status.removedPackages ?? []).map((entry) => ({ ...entry })),
  };
}

const PROVISIONING_RESYNC_INTERVAL_MS = 15 * 60_000;
let resyncTimer: ReturnType<typeof setInterval> | null = null;

function pathCtx(): AgentPathContext {
  return { home: homedir(), env: process.env };
}

export function initProvisioningLifecycle(): void {
  ipcMain.handle('provisioning:status', () => getProvisioningStatus());
  ipcMain.handle('provisioning:set-workspace', (_event, workspaceId: unknown) => {
    void bindProvisioningWorkspace(
      typeof workspaceId === 'string' && workspaceId.length > 0 ? workspaceId : null,
    );
  });
  ipcMain.handle('provisioning:retry', () => retryProvisioning());
  ipcMain.handle('provisioning:access-revoked', (_event, workspaceId: unknown) =>
    runWorkspaceAccessRevokedCleanup(
      typeof workspaceId === 'string' && workspaceId.length > 0 ? workspaceId : null,
    ),
  );

  if (resyncTimer) return;
  resyncTimer = setInterval(() => {
    void syncProvisionedPackages({ ctx: pathCtx() }).catch((err) => {
      console.warn('[provisioning] periodic sync failed:', err);
    });
  }, PROVISIONING_RESYNC_INTERVAL_MS);
  resyncTimer.unref?.();
}

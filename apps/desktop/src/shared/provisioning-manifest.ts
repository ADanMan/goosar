// Типизированный фасад над scripts/provisioning-manifest.mjs.
import {
  PACKAGE_MANIFEST_VERSION as PACKAGE_MANIFEST_VERSION_IMPL,
  PACKAGE_PLATFORMS as PACKAGE_PLATFORMS_IMPL,
  PACKAGE_TYPES as PACKAGE_TYPES_IMPL,
  parsePackageManifest as parsePackageManifestImpl,
} from '../../scripts/provisioning-manifest.mjs';

export const PACKAGE_MANIFEST_VERSION: number = PACKAGE_MANIFEST_VERSION_IMPL;

export type PackageType = 'skill' | 'mcp-server' | 'runtime';

export const PACKAGE_TYPES = PACKAGE_TYPES_IMPL as readonly PackageType[];

export type PackagePlatform =
  '*' | 'darwin-arm64' | 'darwin-x64' | 'win-x64' | 'linux-x64' | 'linux-arm64';

export const PACKAGE_PLATFORMS = PACKAGE_PLATFORMS_IMPL as readonly PackagePlatform[];

export interface PackageManifest {
  schemaVersion: number;
  name: string;
  version: string;
  type: PackageType;
  platform: PackagePlatform;
  sha256: string;
  size: number;
  requires: string[];
  command?: string;
  args?: string[];
  cwd?: string;
}

export function parsePackageManifest(raw: unknown): PackageManifest {
  return parsePackageManifestImpl(raw) as PackageManifest;
}

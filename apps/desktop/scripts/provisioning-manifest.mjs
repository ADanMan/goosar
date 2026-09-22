// Каноническая схема и валидатор манифеста пакета provisioning.

export const PACKAGE_MANIFEST_VERSION = 1;

export const PACKAGE_TYPES = ['skill', 'mcp-server', 'runtime'];

export const PACKAGE_PLATFORMS = [
  '*',
  'darwin-arm64',
  'darwin-x64',
  'win-x64',
  'linux-x64',
  'linux-arm64',
];

const NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/;

const SHA256_RE = /^[0-9a-f]{64}$/;

const REQUIRES_RE = new RegExp(
  `^(${PACKAGE_TYPES.join('|')}):([a-zA-Z0-9][a-zA-Z0-9._-]*)@(\\S+)$`,
);

function describe(value) {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

export function assertValidPackageName(name, field = 'name') {
  if (typeof name !== 'string' || !NAME_RE.test(name)) {
    throw new Error(
      `${field} must be a non-empty string starting with a letter or digit ` +
        `(letters, digits, '.', '-', '_' only), got ${describe(name)}`,
    );
  }
}

export function assertValidPackageVersion(version, field = 'version') {
  if (
    typeof version !== 'string' ||
    version.length === 0 ||
    /\s/.test(version) ||
    version.includes('@')
  ) {
    throw new Error(
      `${field} must be a non-empty string with no whitespace or '@', got ${describe(version)}`,
    );
  }
}

export function assertValidRequiresEntry(entry) {
  if (typeof entry !== 'string' || !REQUIRES_RE.test(entry)) {
    throw new Error(
      'requires entries must have the shape <type>:<name>@<version> where ' +
        `type is one of ${PACKAGE_TYPES.join(', ')}, got ${describe(entry)}`,
    );
  }
}

export function parsePackageManifest(raw) {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error(`package manifest must be a JSON object, got ${describe(raw)}`);
  }

  const {
    schemaVersion,
    name,
    version,
    type,
    platform,
    sha256,
    size,
    requires,
    command,
    args,
    cwd,
  } = raw;

  if (schemaVersion !== PACKAGE_MANIFEST_VERSION) {
    throw new Error(
      `package manifest schemaVersion must be ${PACKAGE_MANIFEST_VERSION}, got ${describe(schemaVersion)}`,
    );
  }

  assertValidPackageName(name, 'name');
  assertValidPackageVersion(version, 'version');

  if (!PACKAGE_TYPES.includes(type)) {
    throw new Error(
      `package manifest type must be one of ${PACKAGE_TYPES.join(', ')}, got ${describe(type)}`,
    );
  }

  if (!PACKAGE_PLATFORMS.includes(platform)) {
    throw new Error(
      `package manifest platform must be one of ${PACKAGE_PLATFORMS.join(', ')}, got ${describe(platform)}`,
    );
  }

  if (typeof sha256 !== 'string' || !SHA256_RE.test(sha256)) {
    throw new Error(
      `package manifest sha256 must be a 64-character lowercase hex string, got ${describe(sha256)}`,
    );
  }

  if (typeof size !== 'number' || !Number.isInteger(size) || size < 0) {
    throw new Error(`package manifest size must be a non-negative integer, got ${describe(size)}`);
  }

  if (!Array.isArray(requires)) {
    throw new Error(
      `package manifest requires must be an array of strings, got ${describe(requires)}`,
    );
  }
  for (const entry of requires) {
    assertValidRequiresEntry(entry);
  }

  if (command !== undefined && (typeof command !== 'string' || command.length === 0)) {
    throw new Error(
      `package manifest command must be a non-empty string, got ${describe(command)}`,
    );
  }
  if (args !== undefined) {
    if (!Array.isArray(args) || args.some((a) => typeof a !== 'string')) {
      throw new Error(`package manifest args must be an array of strings, got ${describe(args)}`);
    }
  }
  if (cwd !== undefined && (typeof cwd !== 'string' || cwd.length === 0)) {
    throw new Error(`package manifest cwd must be a non-empty string, got ${describe(cwd)}`);
  }

  return {
    schemaVersion,
    name,
    version,
    type,
    platform,
    sha256,
    size,
    requires: [...requires],
    ...(command !== undefined ? { command } : {}),
    ...(args !== undefined ? { args: [...args] } : {}),
    ...(cwd !== undefined ? { cwd } : {}),
  };
}

export function buildPackageManifest({
  schemaVersion = PACKAGE_MANIFEST_VERSION,
  name,
  version,
  type,
  platform,
  sha256,
  size,
  requires = [],
  command,
  args,
  cwd,
}) {
  return parsePackageManifest({
    schemaVersion,
    name,
    version,
    type,
    platform,
    sha256,
    size,
    requires,
    ...(command !== undefined ? { command } : {}),
    ...(args !== undefined ? { args } : {}),
    ...(cwd !== undefined ? { cwd } : {}),
  });
}

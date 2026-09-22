import type {
  ProvisioningCatalogPackage,
  ProvisioningPin,
  ProvisioningPinInput,
} from '../api/workspace-admin';

export interface PackageIdentity {
  readonly type: string;
  readonly name: string;
  readonly version: string;
}

export function packageKey(pkg: PackageIdentity): string {
  return `${pkg.type}:${pkg.name}@${pkg.version}`;
}

function pinSlot(pkg: { type: string; name: string }): string {
  return `${pkg.type}:${pkg.name}`;
}

function manifestSlot(name: string, version: string): string {
  return `${name}@${version}`;
}

export type CatalogState =
  | { readonly known: false }
  | {
      readonly known: true;
      readonly packages: readonly ProvisioningCatalogPackage[];
    };

export const UNKNOWN_CATALOG: CatalogState = { known: false };

export function knownCatalog(packages: readonly ProvisioningCatalogPackage[]): CatalogState {
  return { known: true, packages };
}

export type PinCatalogStatus =
  | 'unknown'
  /** This exact (name, version) is published with this type. */
  | 'published'
  /** The name is published, this version is not — the server still 502s. */
  | 'version-missing'
  /** The name is not published at all. */
  | 'missing';

export function pinCatalogStatus(
  pin: { package_type: string; package_name: string; version: string },
  catalog: CatalogState,
): PinCatalogStatus {
  if (!catalog.known) return 'unknown';
  for (const pkg of catalog.packages) {
    if (
      pkg.name === pin.package_name &&
      pkg.version === pin.version &&
      pkg.type === pin.package_type
    ) {
      return 'published';
    }
  }
  for (const pkg of catalog.packages) {
    if (pkg.name === pin.package_name) return 'version-missing';
  }
  return 'missing';
}

export type ServedSet =
  | { readonly status: 'unknown' }
  /** Resolution fails server-side: every machine of the workspace gets a 502
   *  and provisioning is down until the offending pin changes. */
  | { readonly status: 'broken'; readonly blocker: PackageIdentity }
  | {
      readonly status: 'served';
      readonly packages: readonly PackageIdentity[];
    };

const REQUIRE_REF =
  /^([A-Za-z-]+):([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)@([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)$/;

function parseRequireRef(ref: string): PackageIdentity | null {
  const m = REQUIRE_REF.exec(ref);
  if (m === null) return null;
  return { type: m[1]!, name: m[2]!, version: m[3]! };
}

function identityOf(pkg: ProvisioningCatalogPackage): PackageIdentity {
  return { type: pkg.type, name: pkg.name, version: pkg.version };
}

export function resolveServed(pins: readonly ProvisioningPin[], catalog: CatalogState): ServedSet {
  if (!catalog.known) return { status: 'unknown' };

  const byManifest = new Map<string, ProvisioningCatalogPackage>();
  for (const pkg of catalog.packages) {
    const slot = manifestSlot(pkg.name, pkg.version);
    if (!byManifest.has(slot)) byManifest.set(slot, pkg);
  }

  let roots: ProvisioningCatalogPackage[];
  if (pins.length === 0) {
    roots = [...catalog.packages];
  } else {
    roots = [];
    for (const pin of pins) {
      if (pin.enabled !== true) continue; 
      const found = byManifest.get(manifestSlot(pin.package_name, pin.version));
      if (found === undefined || found.type !== pin.package_type) {
        return {
          status: 'broken',
          blocker: {
            type: pin.package_type,
            name: pin.package_name,
            version: pin.version,
          },
        };
      }
      roots.push(found);
    }
  }

  return resolveRequires(roots, byManifest);
}

function resolveRequires(
  roots: readonly ProvisioningCatalogPackage[],
  byManifest: ReadonlyMap<string, ProvisioningCatalogPackage>,
): ServedSet {
  const resolved = new Map<string, PackageIdentity>();
  const visiting = new Set<string>();
  let blocker: PackageIdentity | null = null;

  const visit = (pkg: ProvisioningCatalogPackage): boolean => {
    const identity = identityOf(pkg);
    const key = packageKey(identity);
    if (resolved.has(key)) return true;
    if (visiting.has(key)) {
      blocker = identity; 
      return false;
    }
    visiting.add(key);
    for (const ref of pkg.requires ?? []) {
      const parsed = parseRequireRef(ref);
      if (parsed === null) {
        blocker = identity; 
        visiting.delete(key);
        return false;
      }
      const dep = byManifest.get(manifestSlot(parsed.name, parsed.version));
      if (dep === undefined || dep.type !== parsed.type) {
        blocker = parsed; 
        visiting.delete(key);
        return false;
      }
      if (!visit(dep)) {
        visiting.delete(key);
        return false;
      }
    }
    visiting.delete(key);
    resolved.set(key, identity);
    return true;
  };

  for (const root of roots) {
    if (!visit(root)) {
      return { status: 'broken', blocker: blocker ?? identityOf(root) };
    }
  }
  return { status: 'served', packages: [...resolved.values()] };
}

export type PinAction =
  | { readonly kind: 'enable'; readonly target: PackageIdentity }
  | { readonly kind: 'disable'; readonly target: PackageIdentity }
  | { readonly kind: 'unpin'; readonly target: PackageIdentity }
  | { readonly kind: 'reset' };

function toInput(pin: ProvisioningPin): ProvisioningPinInput {
  return {
    package_name: pin.package_name,
    package_type: pin.package_type,
    version: pin.version,
    enabled: pin.enabled === true,
  };
}

function isTarget(pin: ProvisioningPin, target: PackageIdentity): boolean {
  return pinSlot({ type: pin.package_type, name: pin.package_name }) === pinSlot(target);
}

export function nextPinsFor(
  pins: readonly ProvisioningPin[],
  action: PinAction,
): ProvisioningPinInput[] {
  switch (action.kind) {
    case 'reset':
      return [];
    case 'unpin':
      return pins.filter((p) => !isTarget(p, action.target)).map((p) => toInput(p));
    case 'disable':
      return pins.map((p) =>
        isTarget(p, action.target) ? { ...toInput(p), enabled: false } : toInput(p),
      );
    case 'enable': {
      if (pins.some((p) => isTarget(p, action.target))) {
        return pins.map((p) =>
          isTarget(p, action.target) ? { ...toInput(p), enabled: true } : toInput(p),
        );
      }
      return [
        ...pins.map((p) => toInput(p)),
        {
          package_name: action.target.name,
          package_type: action.target.type,
          version: action.target.version,
          enabled: true,
        },
      ];
    }
  }
}

export type PinEffect =
  | 'unknown'
  /** Provisioning is healthy now and this change takes it down workspace-wide. */
  | 'breaks'
  /** Provisioning is already down and this change does not lift the outage. */
  | 'still-broken'
  /** Provisioning is down now and this change restores delivery. */
  | 'repair'
  /** After the change the machines are served nothing at all. */
  | 'stop'
  /** Packages leave delivery and are deleted off members' machines. */
  | 'narrow'
  /** Packages start being delivered; nothing leaves. */
  | 'widen'
  /** Machines receive exactly what they receive today. */
  | 'none';

export type PinGate =
  | 'blocked'
  /** Typed confirmation against the WORKSPACE name. */
  | 'workspace'
  /** Typed confirmation against the package name. */
  | 'package'
  /** One click. */
  | 'none';

export interface PinOutcome {
  readonly action: PinAction;
  readonly nextPins: ProvisioningPinInput[];
  readonly servedBefore: ServedSet;
  readonly servedAfter: ServedSet;
  readonly revoked: readonly PackageIdentity[];
  readonly restored: readonly PackageIdentity[];
  readonly targetRevoked: boolean;
  readonly targetKept: boolean;
  readonly staleTargetDropped: boolean;
  readonly brokenNow: boolean;
  readonly breaksProvisioning: boolean;
  readonly isLastEnabled: boolean;
  readonly isLastPin: boolean;
  readonly becomesUnpinned: boolean;
  readonly becomesCurated: boolean;
  readonly stopsDelivery: boolean;
  readonly effect: PinEffect;
  readonly gate: PinGate;
}

function diff(from: readonly PackageIdentity[], to: readonly PackageIdentity[]): PackageIdentity[] {
  const keys = new Set(to.map((p) => packageKey(p)));
  return from.filter((p) => !keys.has(packageKey(p)));
}

function contains(set: readonly PackageIdentity[], target: PackageIdentity): boolean {
  return set.some((p) => p.type === target.type && p.name === target.name);
}

export function isEnableBlocked(target: PackageIdentity, catalog: CatalogState): boolean {
  const status = pinCatalogStatus(
    {
      package_type: target.type,
      package_name: target.name,
      version: target.version,
    },
    catalog,
  );
  return status === 'missing' || status === 'version-missing';
}

export function pinOutcome(
  pins: readonly ProvisioningPin[],
  catalog: CatalogState,
  action: PinAction,
): PinOutcome {
  const nextPins = nextPinsFor(pins, action);
  const servedBefore = resolveServed(pins, catalog);
  const servedAfter = resolveServed(nextPins, catalog);

  const enabled = pins.filter((p) => p.enabled === true);
  const nextEnabled = nextPins.filter((p) => p.enabled === true);
  const target = action.kind === 'reset' ? null : action.target;

  const isLastEnabled =
    target !== null &&
    enabled.length === 1 &&
    isTarget(enabled[0]!, target) &&
    nextEnabled.length === 0;
  const isLastPin = target !== null && pins.length === 1 && isTarget(pins[0]!, target);

  const becomesUnpinned = pins.length > 0 && nextPins.length === 0;
  const becomesCurated = pins.length === 0 && nextPins.length > 0;
  const stopsDelivery = enabled.length > 0 && nextEnabled.length === 0 && nextPins.length > 0;

  const brokenNow = servedBefore.status === 'broken';
  const breaksProvisioning = servedAfter.status === 'broken';

  const beforePackages = servedBefore.status === 'served' ? servedBefore.packages : [];
  const afterPackages = servedAfter.status === 'served' ? servedAfter.packages : [];

  const bothResolved = servedBefore.status === 'served' && servedAfter.status === 'served';
  const revoked = bothResolved ? diff(beforePackages, afterPackages) : [];
  const restored =
    servedAfter.status !== 'served'
      ? []
      : servedBefore.status === 'served'
        ? diff(afterPackages, beforePackages)
        : [...afterPackages];

  const targetRevoked = target !== null && contains(revoked, target);
  const targetKept =
    target !== null && servedAfter.status === 'served' && contains(afterPackages, target);
  const targetStatus =
    target === null
      ? 'unknown'
      : pinCatalogStatus(
          {
            package_type: target.type,
            package_name: target.name,
            version: target.version,
          },
          catalog,
        );
  const staleTargetDropped =
    target !== null &&
    (targetStatus === 'missing' || targetStatus === 'version-missing') &&
    servedAfter.status === 'served' &&
    !contains(afterPackages, target) &&
    (brokenNow || contains(beforePackages, target));

  const effect = ((): PinEffect => {
    if (servedBefore.status === 'unknown' || servedAfter.status === 'unknown') {
      return 'unknown';
    }
    if (breaksProvisioning) return brokenNow ? 'still-broken' : 'breaks';
    if (afterPackages.length === 0) {
      return brokenNow || beforePackages.length > 0 ? 'stop' : 'none';
    }
    if (brokenNow) return 'repair';
    if (revoked.length > 0) return 'narrow';
    if (restored.length > 0) return 'widen';
    return 'none';
  })();

  const gate = ((): PinGate => {
    if (action.kind === 'enable') {
      if (breaksProvisioning && !brokenNow) return 'blocked';
      return becomesCurated ? 'workspace' : 'none';
    }
    if (action.kind === 'reset') return 'workspace';
    if (becomesUnpinned || stopsDelivery || isLastEnabled || isLastPin) {
      return 'workspace';
    }
    return 'package';
  })();

  return {
    action,
    nextPins,
    servedBefore,
    servedAfter,
    revoked,
    restored,
    targetRevoked,
    targetKept,
    staleTargetDropped,
    brokenNow,
    breaksProvisioning,
    isLastEnabled,
    isLastPin,
    becomesUnpinned,
    becomesCurated,
    stopsDelivery,
    effect,
    gate,
  };
}

export function provisioningBlocker(
  pins: readonly ProvisioningPin[],
  catalog: CatalogState,
): PackageIdentity | null {
  const served = resolveServed(pins, catalog);
  return served.status === 'broken' ? served.blocker : null;
}

export function blockerIsPinned(
  pins: readonly ProvisioningPin[],
  blocker: PackageIdentity,
): boolean {
  const wanted = packageKey(blocker);
  return pins.some(
    (pin) =>
      packageKey({
        type: pin.package_type,
        name: pin.package_name,
        version: pin.version,
      }) === wanted,
  );
}

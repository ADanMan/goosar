import { describe, it, expect } from 'vitest';
import type { ProvisioningCatalogPackage, ProvisioningPin } from '../api/workspace-admin';
import {
  UNKNOWN_CATALOG,
  knownCatalog,
  packageKey,
  pinCatalogStatus,
  pinOutcome,
  provisioningBlocker,
  resolveServed,
  type PackageIdentity,
} from './pin-outcome';

function pkg(
  name: string,
  version: string,
  type: string,
  requires?: string[],
): ProvisioningCatalogPackage {
  return { name, version, type, platform: '*', ...(requires ? { requires } : {}) };
}

function pin(name: string, version: string, type: string, enabled = true): ProvisioningPin {
  return { package_name: name, package_type: type, version, enabled };
}

function id(type: string, name: string, version: string): PackageIdentity {
  return { type, name, version };
}

function keys(set: readonly PackageIdentity[]): string[] {
  return set.map((p) => packageKey(p)).sort();
}

const DOCX = pkg('docx-skill', '1.2.0', 'skill');
const PYTHON = pkg('python', '3.12.1', 'runtime');
const DOCX_WITH_DEP = pkg('docx-skill', '1.2.0', 'skill', ['runtime:python@3.12.1']);

describe('resolveServed', () => {
  it('serves the whole catalog when the workspace has no pins', () => {
    const served = resolveServed([], knownCatalog([DOCX, PYTHON]));
    expect(served.status).toBe('served');
    expect(served.status === 'served' && keys(served.packages)).toEqual([
      'runtime:python@3.12.1',
      'skill:docx-skill@1.2.0',
    ]);
  });

  it('serves only ENABLED pins once any pin exists', () => {
    const served = resolveServed(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime', false)],
      knownCatalog([DOCX, PYTHON]),
    );
    expect(served.status === 'served' && keys(served.packages)).toEqual(['skill:docx-skill@1.2.0']);
  });

  it('serves nothing when every pin is disabled — pins exist, so no fallback', () => {
    const served = resolveServed(
      [pin('docx-skill', '1.2.0', 'skill', false)],
      knownCatalog([DOCX, PYTHON]),
    );
    expect(served.status === 'served' && served.packages).toEqual([]);
  });

  it('pulls the transitive requires closure in with the roots', () => {
    const served = resolveServed(
      [pin('docx-skill', '1.2.0', 'skill')],
      knownCatalog([DOCX_WITH_DEP, PYTHON]),
    );
    expect(served.status === 'served' && keys(served.packages)).toEqual([
      'runtime:python@3.12.1',
      'skill:docx-skill@1.2.0',
    ]);
  });

  it("breaks when an enabled pin's exact VERSION is not published", () => {
    const served = resolveServed(
      [pin('docx-skill', '1.2.0', 'skill')],
      knownCatalog([pkg('docx-skill', '2.0.0', 'skill'), PYTHON]),
    );
    expect(served.status).toBe('broken');
    expect(served.status === 'broken' && served.blocker).toEqual(
      id('skill', 'docx-skill', '1.2.0'),
    );
  });

  it('breaks when the manifest declares another type than the pin', () => {
    const served = resolveServed([pin('docx-skill', '1.2.0', 'runtime')], knownCatalog([DOCX]));
    expect(served.status).toBe('broken');
  });

  it('breaks on a dangling requires entry', () => {
    const served = resolveServed(
      [pin('docx-skill', '1.2.0', 'skill')],
      knownCatalog([DOCX_WITH_DEP]),
    );
    expect(served.status).toBe('broken');
    expect(served.status === 'broken' && served.blocker).toEqual(id('runtime', 'python', '3.12.1'));
  });

  it('breaks on a requires cycle instead of flattening it', () => {
    const a = pkg('a', '1.0.0', 'skill', ['skill:b@1.0.0']);
    const b = pkg('b', '1.0.0', 'skill', ['skill:a@1.0.0']);
    expect(resolveServed([pin('a', '1.0.0', 'skill')], knownCatalog([a, b])).status).toBe('broken');
  });

  it('cannot answer at all while the catalog is unknown', () => {
    expect(resolveServed([pin('docx-skill', '1.2.0', 'skill')], UNKNOWN_CATALOG).status).toBe(
      'unknown',
    );
  });
});

describe('pinCatalogStatus', () => {
  const catalog = knownCatalog([pkg('docx-skill', '2.0.0', 'skill'), PYTHON]);

  it('reports a published pair as published', () => {
    expect(pinCatalogStatus(pin('python', '3.12.1', 'runtime'), catalog)).toBe('published');
  });

  it('reports a pinned version that is no longer published', () => {
    expect(pinCatalogStatus(pin('docx-skill', '1.2.0', 'skill'), catalog)).toBe('version-missing');
  });

  it('reports a name that is gone entirely', () => {
    expect(pinCatalogStatus(pin('gone', '1.0.0', 'skill'), catalog)).toBe('missing');
  });

  it('claims nothing while the catalog is unknown', () => {
    expect(pinCatalogStatus(pin('docx-skill', '1.2.0', 'skill'), UNKNOWN_CATALOG)).toBe('unknown');
  });
});

describe('pinOutcome: enabling', () => {
  it('pinning the FIRST package narrows delivery and revokes the rest', () => {
    const out = pinOutcome([], knownCatalog([DOCX, PYTHON]), {
      kind: 'enable',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.effect).toBe('narrow');
    expect(out.gate).toBe('workspace');
    expect(out.becomesCurated).toBe(true);
    expect(keys(out.revoked)).toEqual(['runtime:python@3.12.1']);
    expect(out.nextPins).toEqual([
      {
        package_name: 'docx-skill',
        package_type: 'skill',
        version: '1.2.0',
        enabled: true,
      },
    ]);
  });

  it('does not claim the first pin is the ONLY package: its requires survive', () => {
    const out = pinOutcome([], knownCatalog([DOCX_WITH_DEP, PYTHON]), {
      kind: 'enable',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.effect).toBe('none');
    expect(out.revoked).toEqual([]);
    expect(out.servedAfter.status === 'served' && keys(out.servedAfter.packages)).toEqual([
      'runtime:python@3.12.1',
      'skill:docx-skill@1.2.0',
    ]);
  });

  it('adding a pin next to existing ones only widens delivery', () => {
    const out = pinOutcome([pin('docx-skill', '1.2.0', 'skill')], knownCatalog([DOCX, PYTHON]), {
      kind: 'enable',
      target: id('runtime', 'python', '3.12.1'),
    });
    expect(out.effect).toBe('widen');
    expect(out.gate).toBe('none');
    expect(out.revoked).toEqual([]);
    expect(keys(out.restored)).toEqual(['runtime:python@3.12.1']);
  });

  it('re-enabling a disabled pin is one click', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill', false)],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'enable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.gate).toBe('none');
    expect(out.effect).toBe('widen');
  });

  it('REFUSES to enable a pin whose name left the catalog', () => {
    const out = pinOutcome([pin('docx-skill', '1.2.0', 'skill', false)], knownCatalog([PYTHON]), {
      kind: 'enable',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.gate).toBe('blocked');
    expect(out.breaksProvisioning).toBe(true);
  });

  it('REFUSES to enable a pin whose VERSION left the catalog', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill', false)],
      knownCatalog([pkg('docx-skill', '2.0.0', 'skill'), PYTHON]),
      { kind: 'enable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.gate).toBe('blocked');
    expect(out.breaksProvisioning).toBe(true);
    expect(out.effect).toBe('breaks');
  });

  it('does not refuse anything while the catalog is unknown', () => {
    const out = pinOutcome([pin('docx-skill', '1.2.0', 'skill', false)], UNKNOWN_CATALOG, {
      kind: 'enable',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.gate).not.toBe('blocked');
    expect(out.effect).toBe('unknown');
  });
});

describe('pinOutcome: disabling', () => {
  it('disabling one pin while others deliver revokes just that package', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'disable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.effect).toBe('narrow');
    expect(out.gate).toBe('package');
    expect(out.targetRevoked).toBe(true);
    expect(out.targetKept).toBe(false);
  });

  it('does NOT revoke a package another enabled pin requires', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DOCX_WITH_DEP, PYTHON]),
      { kind: 'disable', target: id('runtime', 'python', '3.12.1') },
    );
    expect(out.effect).toBe('none');
    expect(out.revoked).toEqual([]);
    expect(out.targetKept).toBe(true);
    expect(out.targetRevoked).toBe(false);
  });

  it('disabling the last ENABLED pin stops delivery even with disabled pins left', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime', false)],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'disable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.isLastEnabled).toBe(true);
    expect(out.isLastPin).toBe(false);
    expect(out.stopsDelivery).toBe(true);
    expect(out.effect).toBe('stop');
    expect(out.gate).toBe('workspace');
  });
});

describe('pinOutcome: unpinning', () => {
  it('unpinning one pin while others deliver revokes just that package', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'unpin', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.effect).toBe('narrow');
    expect(out.gate).toBe('package');
    expect(out.nextPins).toEqual([
      {
        package_name: 'python',
        package_type: 'runtime',
        version: '3.12.1',
        enabled: true,
      },
    ]);
  });

  it('unpinning an already disabled pin changes nothing on any machine', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill', false), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'unpin', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.effect).toBe('none');
    expect(out.revoked).toEqual([]);
    expect(out.restored).toEqual([]);
    expect(out.gate).toBe('package');
  });

  it('unpinning the LAST pin widens delivery back to the whole catalog', () => {
    const out = pinOutcome([pin('docx-skill', '1.2.0', 'skill')], knownCatalog([DOCX, PYTHON]), {
      kind: 'unpin',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.becomesUnpinned).toBe(true);
    expect(out.effect).toBe('widen');
    expect(out.gate).toBe('workspace');
    expect(out.revoked).toEqual([]);
    expect(keys(out.restored)).toEqual(['runtime:python@3.12.1']);
  });

  it('unpinning the last ENABLED pin leaves the machines with NOTHING', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime', false)],
      knownCatalog([DOCX, PYTHON]),
      { kind: 'unpin', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.isLastEnabled).toBe(true);
    expect(out.isLastPin).toBe(false);
    expect(out.stopsDelivery).toBe(true);
    expect(out.effect).toBe('stop');
    expect(out.gate).toBe('workspace');
  });
});

describe('pinOutcome: an outage in progress', () => {
  const brokenPins = [pin('legacy', '1.0.0', 'skill'), pin('python', '3.12.1', 'runtime')];
  const catalog = knownCatalog([PYTHON]);

  it('knows provisioning is DOWN while an enabled pin is unpublished', () => {
    expect(provisioningBlocker(brokenPins, catalog)).toEqual(id('skill', 'legacy', '1.0.0'));
  });

  it('treats unpinning the broken pin as a REPAIR, not a revocation', () => {
    const out = pinOutcome(brokenPins, catalog, {
      kind: 'unpin',
      target: id('skill', 'legacy', '1.0.0'),
    });
    expect(out.brokenNow).toBe(true);
    expect(out.breaksProvisioning).toBe(false);
    expect(out.effect).toBe('repair');
    expect(keys(out.restored)).toEqual(['runtime:python@3.12.1']);
    expect(out.staleTargetDropped).toBe(true);
  });

  it('treats disabling the broken pin as a repair too', () => {
    const out = pinOutcome(brokenPins, catalog, {
      kind: 'disable',
      target: id('skill', 'legacy', '1.0.0'),
    });
    expect(out.effect).toBe('repair');
    expect(keys(out.restored)).toEqual(['runtime:python@3.12.1']);
  });

  it('says the outage continues when another pin is broken too', () => {
    const out = pinOutcome([...brokenPins, pin('other', '1.0.0', 'skill')], catalog, {
      kind: 'unpin',
      target: id('skill', 'legacy', '1.0.0'),
    });
    expect(out.effect).toBe('still-broken');
    expect(out.gate).toBe('package');
  });

  it('still calls it a stop when the repair leaves nothing enabled', () => {
    const out = pinOutcome(
      [pin('legacy', '1.0.0', 'skill'), pin('python', '3.12.1', 'runtime', false)],
      catalog,
      { kind: 'disable', target: id('skill', 'legacy', '1.0.0') },
    );
    expect(out.effect).toBe('stop');
    expect(out.gate).toBe('workspace');
  });
});

describe('pinOutcome: removing every pin', () => {
  it('widens delivery back to the whole catalog and revokes nothing', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DOCX, PYTHON, pkg('extra', '1.0.0', 'skill')]),
      { kind: 'reset' },
    );
    expect(out.effect).toBe('widen');
    expect(out.gate).toBe('workspace');
    expect(out.revoked).toEqual([]);
    expect(keys(out.restored)).toEqual(['skill:extra@1.0.0']);
    expect(out.nextPins).toEqual([]);
  });

  it('lifts an outage when one of the pins was the unpublished one', () => {
    const out = pinOutcome(
      [pin('legacy', '1.0.0', 'skill'), pin('python', '3.12.1', 'runtime')],
      knownCatalog([PYTHON]),
      { kind: 'reset' },
    );
    expect(out.brokenNow).toBe(true);
    expect(out.effect).toBe('repair');
    expect(out.gate).toBe('workspace');
  });
});

describe('pinOutcome: the catalog itself cannot be resolved', () => {
  const DANGLING = pkg('docx-skill', '1.2.0', 'skill', ['runtime:python@9.9.9']);

  it('REFUSES to enable a pin whose dependency is not published', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill', false), pin('python', '3.12.1', 'runtime')],
      knownCatalog([DANGLING, PYTHON]),
      { kind: 'enable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.breaksProvisioning).toBe(true);
    expect(out.brokenNow).toBe(false);
    expect(out.effect).toBe('breaks');
    expect(out.gate).toBe('blocked');
  });

  it('stops refusing once provisioning is already down anyway', () => {
    const out = pinOutcome(
      [pin('docx-skill', '1.2.0', 'skill', false), pin('legacy', '1.0.0', 'skill')],
      knownCatalog([DANGLING]),
      { kind: 'enable', target: id('skill', 'docx-skill', '1.2.0') },
    );
    expect(out.brokenNow).toBe(true);
    expect(out.effect).toBe('still-broken');
    expect(out.gate).not.toBe('blocked');
  });

  it('calls dropping the last pin a break when the whole catalog is unresolvable', () => {
    const out = pinOutcome([pin('python', '3.12.1', 'runtime')], knownCatalog([DANGLING, PYTHON]), {
      kind: 'unpin',
      target: id('runtime', 'python', '3.12.1'),
    });
    expect(out.brokenNow).toBe(false);
    expect(out.effect).toBe('breaks');
  });
});

describe('pinOutcome: the deployment publishes nothing', () => {
  it('calls dropping every pin a STOP, not a widening, on an empty catalog', () => {
    const out = pinOutcome(
      [pin('legacy', '1.0.0', 'skill', false), pin('old', '2.0.0', 'skill')],
      knownCatalog([]),
      { kind: 'reset' },
    );
    expect(out.becomesUnpinned).toBe(true);
    expect(out.breaksProvisioning).toBe(false);
    expect(out.effect).toBe('stop');
  });

  it('still calls it nothing-changes when the machines already got nothing', () => {
    const out = pinOutcome([pin('legacy', '1.0.0', 'skill', false)], knownCatalog([]), {
      kind: 'unpin',
      target: id('skill', 'legacy', '1.0.0'),
    });
    expect(out.effect).toBe('none');
  });
});

describe('pinOutcome: pins matched by (type, name), never by identity', () => {
  it('drops the pin a refetch replaced with an equal object', () => {
    const before = [pin('docx-skill', '1.2.0', 'skill'), pin('python', '3.12.1', 'runtime')];
    const after = before.map((p) => ({ ...p }));
    const out = pinOutcome(after, knownCatalog([DOCX, PYTHON]), {
      kind: 'unpin',
      target: id('skill', 'docx-skill', '1.2.0'),
    });
    expect(out.nextPins.map((p) => p.package_name)).toEqual(['python']);
  });

  it('keeps a same-named package of another type', () => {
    const out = pinOutcome(
      [pin('tool', '1.0.0', 'skill'), pin('tool', '1.0.0', 'runtime')],
      knownCatalog([pkg('tool', '1.0.0', 'skill'), pkg('tool', '1.0.0', 'runtime')]),
      { kind: 'unpin', target: id('skill', 'tool', '1.0.0') },
    );
    expect(out.nextPins).toEqual([
      {
        package_name: 'tool',
        package_type: 'runtime',
        version: '1.0.0',
        enabled: true,
      },
    ]);
  });
});

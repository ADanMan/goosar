import { describe, it, expect } from 'vitest';
import { paths } from '@goosar/core/paths';
import { navSectionForPath, navBreadcrumbLabelKey } from './nav-sections';

const p = paths.workspace('acme');

describe('navBreadcrumbLabelKey (T-032 §3.2)', () => {
  it('labels the agents page with the shared crew section label', () => {
    expect(navSectionForPath(p, p.agents())).toBe('crew');
    expect(navBreadcrumbLabelKey(p, p.agents(), 'crew')).toBe('agents');
  });

  it('labels the squads page with its own name instead of the parent section', () => {
    expect(navSectionForPath(p, p.squads())).toBe('crew');
    expect(navBreadcrumbLabelKey(p, p.squads(), 'crew')).toBe('squads');
  });

  it('labels a squad detail page the same as the squads list', () => {
    expect(navBreadcrumbLabelKey(p, p.squadDetail('sq_1'), 'crew')).toBe('squads');
  });

  it('falls back to the section label outside crew', () => {
    expect(navBreadcrumbLabelKey(p, p.usage(), 'settings')).toBe('settings');
    expect(navBreadcrumbLabelKey(p, p.issues(), 'tasks')).toBe('issues');
  });

  it('returns null when there is no active section', () => {
    expect(navBreadcrumbLabelKey(p, '/acme/unknown', null)).toBeNull();
  });
});

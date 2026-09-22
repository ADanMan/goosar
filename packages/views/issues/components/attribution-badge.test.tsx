// @vitest-environment jsdom

import { cleanup, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { TaskAttribution } from '@goosar/core/types';
import { renderWithI18n } from '../../test/i18n';

vi.mock('@goosar/ui/components/common/actor-avatar', () => ({
  ActorAvatar: ({ name }: { name: string }) => <span data-testid="actor-avatar">{name}</span>,
}));

import { AttributionBadge } from './attribution-badge';

afterEach(cleanup);

describe('AttributionBadge', () => {
  it('renders nothing when the task has no attribution', () => {
    const { container } = renderWithI18n(<AttributionBadge />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows who the run is on behalf of, with the source as a tooltip', () => {
    const attribution: TaskAttribution = {
      source: 'direct_human',
      precise: true,
      initiator: { id: 'u1', name: 'Ada Lovelace' },
    };
    renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(screen.getByText('On behalf of Ada Lovelace')).toBeInTheDocument();
    expect(screen.getByTitle('Direct member action')).toBeInTheDocument();
  });

  it('marks a fallback guess (owner_fallback) with a warning tone', () => {
    const attribution: TaskAttribution = {
      source: 'owner_fallback',
      precise: false,
      initiator: { id: 'u2', name: 'Grace' },
    };
    const { container } = renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(screen.getByText('On behalf of Grace')).toBeInTheDocument();
    expect(container.querySelector('.text-warning')).not.toBeNull();
    expect(
      screen.getByTitle('No precise owner — attributed to the agent owner'),
    ).toBeInTheDocument();
  });

  it('shows a backfilled attribution in the normal tone, not a warning (MUL-4768)', () => {
    const attribution: TaskAttribution = {
      source: 'backfill',
      precise: false,
      initiator: { id: 'u5', name: 'Bohan' },
    };
    const { container } = renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(screen.getByText('On behalf of Bohan')).toBeInTheDocument();
    expect(container.querySelector('.text-warning')).toBeNull();
    expect(container.querySelector('.text-muted-foreground')).not.toBeNull();
    expect(screen.getByTitle('Backfilled attribution')).toBeInTheDocument();
  });

  it('avatar variant shows no warning ring for a backfilled attribution (MUL-4768)', () => {
    const attribution: TaskAttribution = {
      source: 'backfill',
      precise: false,
      initiator: { id: 'u5', name: 'Bohan' },
    };
    const { container } = renderWithI18n(
      <AttributionBadge attribution={attribution} variant="avatar" />,
    );

    expect(container.querySelector('.ring-warning\\/60')).toBeNull();
  });

  it('falls back to a generic name when the initiator has no display name', () => {
    const attribution: TaskAttribution = {
      source: 'delegation',
      precise: true,
      initiator: { id: 'u3' },
    };
    renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(screen.getByText('On behalf of someone')).toBeInTheDocument();
  });

  it('inline variant renders the bare name (no on-behalf-of wrapper), source in tooltip', () => {
    const attribution: TaskAttribution = {
      source: 'direct_human',
      precise: true,
      initiator: { id: 'u1', name: 'Ada Lovelace' },
    };
    const { container } = renderWithI18n(
      <AttributionBadge attribution={attribution} variant="inline" />,
    );

    expect(screen.getAllByText('Ada Lovelace').length).toBeGreaterThan(0);
    expect(screen.queryByText('On behalf of Ada Lovelace')).toBeNull();
    expect(container.querySelector("[data-slot='badge']")).toBeNull();
  });

  it('renders nothing when no responsible member resolved (MUL-4765)', () => {
    const attribution: TaskAttribution = {
      source: 'unattributed',
      precise: false,
    };
    const { container } = renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(container).toBeEmptyDOMElement();
  });

  it('degrades gracefully for an unknown source label', () => {
    const attribution: TaskAttribution = {
      source: 'future_source',
      precise: true,
      initiator: { id: 'u4', name: 'Ada' },
    };
    renderWithI18n(<AttributionBadge attribution={attribution} />);

    expect(screen.getByTitle('future_source')).toBeInTheDocument();
  });

  it("avatar variant renders just the accountable member's avatar", () => {
    const attribution: TaskAttribution = {
      source: 'direct_human',
      precise: true,
      initiator: { id: 'u1', name: 'Ada Lovelace' },
    };
    renderWithI18n(<AttributionBadge attribution={attribution} variant="avatar" />);

    expect(screen.getByTestId('actor-avatar')).toHaveTextContent('Ada Lovelace');
    expect(screen.queryByText('On behalf of Ada Lovelace')).toBeNull();
  });

  it('avatar variant renders nothing without an accountable member', () => {
    const attribution: TaskAttribution = {
      source: 'unattributed',
      precise: false,
    };
    const { container } = renderWithI18n(
      <AttributionBadge attribution={attribution} variant="avatar" />,
    );

    expect(container).toBeEmptyDOMElement();
  });
});

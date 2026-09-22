import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ProviderLogo } from './provider-logo';

describe('ProviderLogo', () => {
  it('renders the runtime letter as a neutral badge, not any vendor mark', () => {
    const { container } = render(<ProviderLogo provider="runtime-q" className="runtime-logo" />);

    const svg = container.querySelector('svg');
    expect(svg?.classList.contains('runtime-logo')).toBe(true);
    expect(svg?.textContent).toBe('Q');
    expect(container.querySelector('img')).toBeNull();
  });

  it('gives the same runtime letter the same badge color across renders', () => {
    const first = render(<ProviderLogo provider="runtime-c" />);
    const second = render(<ProviderLogo provider="runtime-c" />);

    const fillClass = (node: Element | null) =>
      Array.from(node?.querySelector('text')?.classList ?? []).find((c) => c.startsWith('fill-'));

    expect(fillClass(first.container.querySelector('svg'))).toBe(
      fillClass(second.container.querySelector('svg')),
    );
  });

  it('falls back to the generic icon for a provider that is not a runtime code', () => {
    const { container } = render(<ProviderLogo provider="totally-unknown" />);

    expect(container.querySelector('svg')).not.toBeNull();
    expect(container.querySelector('text')).toBeNull();
  });
});

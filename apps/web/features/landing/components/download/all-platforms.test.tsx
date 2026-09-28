import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AllPlatforms } from './all-platforms';

vi.mock('../../i18n', () => ({
  useLocale: () => ({
    t: {
      download: {
        allPlatforms: {
          title: 'All platforms',
          macArm64Label: 'macOS · Apple Silicon',
          macX64Label: 'macOS · Intel',
          winX64Label: 'Windows · x64',
          winArm64Label: 'Windows · ARM64',
          linuxX64Label: 'Linux · x64',
          linuxArm64Label: 'Linux · ARM64',
          formatDmg: '.dmg',
          formatZip: '.zip',
          formatExe: '.exe',
          formatAppImage: '.AppImage',
          formatDeb: '.deb',
          formatRpm: '.rpm',
          unavailable: 'Not available',
          fileSizeUnits: { b: 'B', kb: 'KB', mb: 'MB', gb: 'GB' },
          checksumsLabel: 'SHA256 checksums',
        },
        footer: { allReleases: 'View all releases' },
      },
    },
  }),
}));

describe('AllPlatforms', () => {
  it('lists Intel macOS downloads separately from Apple Silicon', () => {
    render(
      <AllPlatforms
        assets={{
          macArm64Dmg: 'https://downloads.test/mac-arm64.dmg',
          macArm64Zip: 'https://downloads.test/mac-arm64.zip',
          macX64Dmg: 'https://downloads.test/mac-x64.dmg',
          macX64Zip: 'https://downloads.test/mac-x64.zip',
        }}
        fallbackHref="https://github.test/releases"
      />,
    );

    const intelLabel = screen.getByText('macOS · Intel');
    const intelRow = intelLabel.parentElement?.parentElement;
    expect(intelRow).not.toBeNull();
    expect(within(intelRow!).getByRole('link', { name: '.dmg' })).toHaveAttribute(
      'href',
      'https://downloads.test/mac-x64.dmg',
    );
    expect(within(intelRow!).getByRole('link', { name: '.zip' })).toHaveAttribute(
      'href',
      'https://downloads.test/mac-x64.zip',
    );
  });

  it('shows a human-readable size next to a link when the asset size is known', () => {
    render(
      <AllPlatforms
        assets={{ macArm64Dmg: 'https://downloads.test/mac-arm64.dmg' }}
        assetSizes={{ macArm64Dmg: 104_857_600 }}
        fallbackHref="https://github.test/releases"
      />,
    );

    const link = screen.getByRole('link', { name: /\.dmg/ });
    expect(link).toHaveTextContent('100 MB');
  });

  it('shows no size when it is unknown for an asset', () => {
    render(
      <AllPlatforms
        assets={{ macArm64Dmg: 'https://downloads.test/mac-arm64.dmg' }}
        fallbackHref="https://github.test/releases"
      />,
    );

    const link = screen.getByRole('link', { name: '.dmg' });
    expect(link).toHaveTextContent('.dmg');
  });

  it('links to the checksums asset when the release publishes one', () => {
    render(
      <AllPlatforms
        assets={{}}
        checksumsUrl="https://downloads.test/SHA256SUMS.txt"
        fallbackHref="https://github.test/releases"
      />,
    );

    expect(screen.getByRole('link', { name: 'SHA256 checksums' })).toHaveAttribute(
      'href',
      'https://downloads.test/SHA256SUMS.txt',
    );
  });

  it('shows no checksums link when the release does not publish one', () => {
    render(<AllPlatforms assets={{}} fallbackHref="https://github.test/releases" />);

    expect(screen.queryByRole('link', { name: 'SHA256 checksums' })).not.toBeInTheDocument();
  });
});

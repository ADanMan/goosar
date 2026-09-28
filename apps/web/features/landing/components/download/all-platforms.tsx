import Link from 'next/link';
import { useLocale } from '../../i18n';
import { formatFileSize } from '../../utils/format-file-size';
import type {
  DownloadAssetKey,
  DownloadAssetSizes,
  DownloadAssets,
} from '../../utils/parse-release-assets';
import { AppleIcon, LinuxIcon, WindowsIcon } from './os-icons';

interface Props {
  assets: DownloadAssets;
  assetSizes?: DownloadAssetSizes;
  checksumsUrl?: string | null;
  fallbackHref: string;
}

export function AllPlatforms({ assets, assetSizes = {}, checksumsUrl, fallbackHref }: Props) {
  const { t } = useLocale();
  const d = t.download.allPlatforms;

  const sizeFor = (key: DownloadAssetKey) => {
    const bytes = assetSizes[key];
    return bytes ? formatFileSize(bytes, d.fileSizeUnits) : null;
  };

  return (
    <section id="all-platforms" className="bg-background py-20 text-foreground sm:py-24">
      <div className="mx-auto max-w-[920px] px-4 sm:px-6 lg:px-8">
        <h2 className="font-[family-name:var(--font-serif)] text-xl leading-[1.1] tracking-[-0.03em] sm:text-2xl">
          {d.title}
        </h2>

        <div className="mt-10 overflow-hidden rounded-2xl border border-border">
          <Row
            icon={<AppleIcon className="text-foreground" />}
            label={d.macArm64Label}
            formats={[
              {
                label: d.formatDmg,
                href: assets.macArm64Dmg,
                size: sizeFor('macArm64Dmg'),
              },
              {
                label: d.formatZip,
                href: assets.macArm64Zip,
                size: sizeFor('macArm64Zip'),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<AppleIcon className="text-foreground" />}
            label={d.macX64Label}
            formats={[
              {
                label: d.formatDmg,
                href: assets.macX64Dmg,
                size: sizeFor('macX64Dmg'),
              },
              {
                label: d.formatZip,
                href: assets.macX64Zip,
                size: sizeFor('macX64Zip'),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<WindowsIcon className="text-foreground" />}
            label={d.winX64Label}
            formats={[
              {
                label: d.formatExe,
                href: assets.winX64Exe,
                size: sizeFor('winX64Exe'),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<WindowsIcon className="text-foreground" />}
            label={d.winArm64Label}
            formats={[
              {
                label: d.formatExe,
                href: assets.winArm64Exe,
                size: sizeFor('winArm64Exe'),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<LinuxIcon className="text-foreground" />}
            label={d.linuxX64Label}
            formats={[
              {
                label: d.formatAppImage,
                href: assets.linuxAmd64AppImage,
                size: sizeFor('linuxAmd64AppImage'),
              },
              {
                label: d.formatDeb,
                href: assets.linuxAmd64Deb,
                size: sizeFor('linuxAmd64Deb'),
              },
              {
                label: d.formatRpm,
                href: assets.linuxAmd64Rpm,
                size: sizeFor('linuxAmd64Rpm'),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<LinuxIcon className="text-foreground" />}
            label={d.linuxArm64Label}
            formats={[
              {
                label: d.formatAppImage,
                href: assets.linuxArm64AppImage,
                size: sizeFor('linuxArm64AppImage'),
              },
              {
                label: d.formatDeb,
                href: assets.linuxArm64Deb,
                size: sizeFor('linuxArm64Deb'),
              },
              {
                label: d.formatRpm,
                href: assets.linuxArm64Rpm,
                size: sizeFor('linuxArm64Rpm'),
              },
            ]}
            unavailable={d.unavailable}
            isLast
          />
        </div>

        {checksumsUrl ? (
          <p className="mt-4 text-xs text-muted-foreground">
            <Link
              href={checksumsUrl}
              className="underline decoration-border underline-offset-4 hover:text-foreground hover:decoration-foreground/70"
              target="_blank"
              rel="noreferrer"
            >
              {d.checksumsLabel}
            </Link>
          </p>
        ) : null}

        {isFallbackNeeded(assets) ? (
          <p className="mt-6 text-xs text-muted-foreground">
            <Link
              href={fallbackHref}
              className="underline decoration-border underline-offset-4 hover:text-foreground hover:decoration-foreground/70"
              target="_blank"
              rel="noreferrer"
            >
              {t.download.footer.allReleases}
            </Link>
          </p>
        ) : null}
      </div>
    </section>
  );
}

interface RowProps {
  icon: React.ReactNode;
  label: string;
  formats: {
    label: string;
    href: string | undefined;
    size?: string | null;
  }[];
  unavailable: string;
  isLast?: boolean;
}

function Row({ icon, label, formats, unavailable, isLast }: RowProps) {
  return (
    <div
      className={`grid grid-cols-1 items-center gap-x-6 gap-y-3 px-6 py-5 sm:grid-cols-[220px_1fr] ${isLast ? '' : 'border-b border-border'}`}
    >
      <div className="flex items-center gap-3">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted">
          {icon}
        </span>
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {formats.map((f) =>
          f.href ? (
            <a
              key={f.label}
              href={f.href}
              className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-3 py-1.5 text-xs font-medium transition-colors hover:border-foreground/30 hover:bg-muted"
            >
              {f.label}
              {f.size ? <span className="text-muted-foreground">{f.size}</span> : null}
            </a>
          ) : (
            <span
              key={f.label}
              aria-disabled="true"
              className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-lg border border-border bg-muted px-3 py-1.5 text-xs text-muted-foreground line-through"
              title={unavailable}
            >
              {f.label}
              <span className="sr-only">{` — ${unavailable}`}</span>
            </span>
          ),
        )}
      </div>
    </div>
  );
}

const EXPECTED_ASSET_COUNT = 12;

function isFallbackNeeded(assets: DownloadAssets): boolean {
  return Object.values(assets).filter(Boolean).length < EXPECTED_ASSET_COUNT;
}

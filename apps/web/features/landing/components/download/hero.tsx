import Link from 'next/link';
import { ArrowRight, Download } from 'lucide-react';
import { useLocale } from '../../i18n';
import type { DetectResult } from '../../utils/os-detect';
import type { DownloadAssets } from '../../utils/parse-release-assets';
import { heroButtonClassName } from '../shared';

interface Props {
  detected: DetectResult | null;
  assets: DownloadAssets;
  versionUnavailable: boolean;
}

export function DownloadHero({ detected, assets, versionUnavailable }: Props) {
  const { t } = useLocale();
  const d = t.download.hero;

  const content = resolveContent(detected, assets, versionUnavailable, d);

  return (
    <section className="relative overflow-hidden bg-inverse text-inverse-foreground">
      <BackdropGradient />
      <div className="relative z-10 mx-auto max-w-[1120px] px-4 pb-24 pt-32 text-center sm:px-6 sm:pt-40 lg:px-8 lg:pb-28">
        <h1 className="mx-auto max-w-[880px] font-[family-name:var(--font-serif)] text-2xl leading-[1.02] tracking-[-0.035em] drop-shadow-lg sm:text-3xl">
          {content.title}
        </h1>
        <p className="mx-auto mt-6 max-w-[620px] text-sm leading-7 text-inverse-foreground sm:text-base">
          {content.sub}
        </p>

        <div className="mt-10 flex flex-wrap items-center justify-center gap-3">
          {content.primary ? (
            <PrimaryCta href={content.primary.href} disabled={content.primary.disabled}>
              <Download className="size-4" aria-hidden />
              {content.primary.label}
              {!content.primary.disabled && <ArrowRight className="size-4" aria-hidden />}
            </PrimaryCta>
          ) : null}
          {content.alt ? (
            <Link href={content.alt.href} className={heroButtonClassName('ghost')}>
              {content.alt.label}
            </Link>
          ) : null}
        </div>

        {content.hint ? (
          <p className="mx-auto mt-5 max-w-[520px] text-xs text-inverse-muted-foreground">
            {content.hint}
          </p>
        ) : null}

        {versionUnavailable ? (
          <p className="mx-auto mt-6 max-w-[520px] text-xs uppercase tracking-[0.14em] text-inverse-muted-foreground">
            {t.download.footer.versionUnavailable}
          </p>
        ) : null}
      </div>
    </section>
  );
}

interface HeroContent {
  title: string;
  sub: string;
  primary?: {
    href: string;
    label: string;
    disabled: boolean;
  };
  alt?: { href: string; label: string };
  hint?: string;
}

type HeroDict = ReturnType<typeof useLocale>['t']['download']['hero'];

export function resolveContent(
  detected: DetectResult | null,
  assets: DownloadAssets,
  versionUnavailable: boolean,
  d: HeroDict,
): HeroContent {
  if (!detected || detected.os === 'unknown') {
    return { title: d.unknown.title, sub: d.unknown.sub };
  }

  if (detected.os === 'mac') {
    if (detected.arch === 'x64' && detected.archConfident) {
      const dmg = assets.macX64Dmg;
      const zip = assets.macX64Zip;
      return {
        title: d.macIntel.title,
        sub: d.macIntel.sub,
        primary: dmg
          ? {
              href: dmg,
              label: d.macIntel.primary,
              disabled: false,
            }
          : versionUnavailable
            ? { href: '#', label: d.macIntel.primary, disabled: true }
            : undefined,
        alt: zip
          ? {
              href: zip,
              label: d.macIntel.altZip,
            }
          : undefined,
      };
    }
    const dmg = assets.macArm64Dmg;
    const zip = assets.macArm64Zip;
    return {
      title: d.macArm64.title,
      sub: d.macArm64.sub,
      primary: dmg
        ? {
            href: dmg,
            label: d.macArm64.primary,
            disabled: false,
          }
        : versionUnavailable
          ? { href: '#', label: d.macArm64.primary, disabled: true }
          : undefined,
      alt: zip
        ? {
            href: zip,
            label: d.macArm64.altZip,
          }
        : undefined,
      hint: detected.archConfident ? undefined : d.safariMacHint,
    };
  }

  if (detected.os === 'windows') {
    const isArm = detected.arch === 'arm64';
    const copy = isArm ? d.winArm64 : d.winX64;
    const url = isArm ? assets.winArm64Exe : assets.winX64Exe;
    return {
      title: copy.title,
      sub: copy.sub,
      primary: url
        ? {
            href: url,
            label: copy.primary,
            disabled: false,
          }
        : versionUnavailable
          ? { href: '#', label: copy.primary, disabled: true }
          : undefined,
      hint: detected.archConfident ? undefined : d.archFallbackHint,
    };
  }

  const isArmLinux = detected.arch === 'arm64';
  const primaryUrl = isArmLinux ? assets.linuxArm64AppImage : assets.linuxAmd64AppImage;
  return {
    title: d.linux.title,
    sub: d.linux.sub,
    primary: primaryUrl
      ? {
          href: primaryUrl,
          label: d.linux.primary,
          disabled: false,
        }
      : versionUnavailable
        ? { href: '#', label: d.linux.primary, disabled: true }
        : undefined,
    alt: { href: '#all-platforms', label: d.linux.altFormats },
    hint: detected.archConfident ? undefined : d.archFallbackHint,
  };
}

function PrimaryCta({
  href,
  disabled,
  children,
}: {
  href: string;
  disabled: boolean;
  children: React.ReactNode;
}) {
  if (disabled) {
    return (
      <span
        aria-disabled="true"
        className="inline-flex h-10 cursor-not-allowed items-center justify-center gap-2 rounded-md border border-inverse-foreground/15 bg-inverse-foreground/5 px-4 text-sm font-medium text-inverse-muted-foreground"
      >
        {children}
      </span>
    );
  }
  return (
    <a href={href} className={heroButtonClassName('solid')}>
      {children}
    </a>
  );
}

function BackdropGradient() {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0"
      style={{
        background:
          'radial-gradient(ellipse 70% 50% at 50% 0%, color-mix(in oklch, var(--brand) 35%, transparent), transparent 60%), radial-gradient(ellipse 50% 40% at 50% 80%, color-mix(in oklch, var(--brand) 15%, transparent), transparent 60%)',
      }}
    />
  );
}

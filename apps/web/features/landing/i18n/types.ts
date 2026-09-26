import type { SupportedLocale } from '@goosar/core/i18n';
import type { DemoLogLineKey } from '../demo-log';

export type Locale = SupportedLocale;
export type LandingDictionaryLocale = 'en' | 'ru';

export const locales: Locale[] = ['en', 'ru'];

export const localeLabels: Partial<Record<Locale, string>> = {
  en: 'EN',
  ru: '\u0420\u0423',
};

export function toLandingDictionaryLocale(locale: Locale): LandingDictionaryLocale {
  return locale === 'ru' ? 'ru' : 'en';
}

type FooterGroup = {
  label: string;
  links: { label: string; href: string }[];
};

export type LandingDict = {
  header: {
    github: string;
    cta: string;
    dashboard: string;
    navigation: string;
    openMenu: string;
    closeMenu: string;
  };
  hero: {
    headlineLine1: string;
    headlineLine2: string;
    subheading: string;
    cta: string;
    downloadDesktop: string;
    worksWith: string;
    runtimeCount: string;
    imageAlt: string;
  };
  demoLog: {
    lines: Record<DemoLogLineKey, string>;
  };
  features: {
    agentExecutor: {
      label: string;
      title: string;
      description: string;
      points: { title: string; description: string }[];
      imageAlt: string;
    };
    selfHosted: {
      label: string;
      title: string;
      description: string;
      points: { title: string; description: string }[];
    };
    hermes: {
      label: string;
      title: string;
      description: string;
      points: { title: string; description: string }[];
      imageAlt: string;
    };
    install: {
      label: string;
      title: string;
      description: string;
      command: string;
      note: string;
    };
  };
  businessRoles: {
    label: string;
    title: string;
    description: string;
    roles: { title: string; description: string }[];
  };
  howItWorks: {
    label: string;
    headlineMain: string;
    headlineFaded: string;
    steps: { title: string; description: string }[];
    cta: string;
    ctaGithub: string;
  };
  openSource: {
    label: string;
    headlineLine1: string;
    headlineLine2: string;
    description: string;
    cta: string;
    highlights: { title: string; description: string }[];
  };
  faq: {
    label: string;
    headline: string;
    items: { question: string; answer: string }[];
  };
  footer: {
    tagline: string;
    cta: string;
    groups: {
      product: FooterGroup;
      resources: FooterGroup;
      company: FooterGroup;
    };
    copyright: string;
  };
  about: {
    title: string;
    paragraphs: string[];
    cta: string;
  };
  download: {
    hero: {
      macArm64: {
        title: string;
        sub: string;
        primary: string;
        altZip: string;
      };
      macIntel: {
        title: string;
        sub: string;
        primary: string;
        altZip: string;
      };
      winX64: { title: string; sub: string; primary: string };
      winArm64: { title: string; sub: string; primary: string };
      linux: {
        title: string;
        sub: string;
        primary: string;
        altFormats: string;
      };
      unknown: { title: string; sub: string };
      safariMacHint: string;
      archFallbackHint: string;
    };
    allPlatforms: {
      title: string;
      macArm64Label: string;
      macX64Label: string;
      winX64Label: string;
      winArm64Label: string;
      linuxX64Label: string;
      linuxArm64Label: string;
      formatDmg: string;
      formatZip: string;
      formatExe: string;
      formatAppImage: string;
      formatDeb: string;
      formatRpm: string;
      unavailable: string;
    };
    cli: {
      title: string;
      sub: string;
      installLabel: string;
      startLabel: string;
      sshNote: string;
      copyLabel: string;
      copiedLabel: string;
    };
    cloud: { title: string; sub: string };
    footer: {
      releaseNotes: string;
      allReleases: string;
      currentVersion: string;
      versionUnavailable: string;
    };
  };
};

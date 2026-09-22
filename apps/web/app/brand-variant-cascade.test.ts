import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import postcss from 'postcss';
import tailwind from '@tailwindcss/postcss';
import { cn } from '@goosar/ui/lib/utils';
import { buttonVariants } from '@goosar/ui/components/ui/button';
import { beforeAll, describe, expect, it } from 'vitest';

interface FlatRule {
  sel: string;
  prop: string;
  order: number;
}

interface ElementState {
  dark?: boolean;
  hover?: boolean;
  active?: boolean;
  expanded?: boolean;
}

const repoRoot = resolve(process.cwd(), '../..');

async function compileStylesheet(entry: string): Promise<FlatRule[]> {
  const css = readFileSync(entry, 'utf8');
  const built = await postcss([tailwind({ base: dirname(entry) })]).process(css, {
    from: entry,
  });

  const rules: FlatRule[] = [];
  let order = 0;
  const walk = (container: postcss.Container, prefix: string) => {
    container.each((node) => {
      if (node.type === 'decl') {
        if (prefix) rules.push({ sel: prefix, prop: node.prop, order: order++ });
      } else if (node.type === 'rule') {
        const sel = node.selector.includes('&')
          ? node.selector.replace(/&/g, prefix)
          : prefix
            ? prefix + node.selector
            : node.selector;
        walk(node, sel);
      } else if (node.type === 'atrule') {
        walk(node, prefix);
      }
    });
  };
  walk(built.root, '');
  return rules;
}

function specificity(selector: string): number {
  let count = 0;
  const withoutIs = selector.replace(/:is\(([^()]*)\)/g, (_, inner: string) => {
    count += Math.max(
      ...inner.split(',').map((part) => (part.match(/\.[^.\s>+~:[]+/g) ?? []).length),
    );
    return '';
  });
  return count + (withoutIs.match(/\\?\.[A-Za-z0-9_\\/:.\-[\]%]+/g) ?? []).length;
}

const CLASS_PREFIX = /^\.((?:[^.\s:[\\]|\\.)+)/;

function baseClassOf(selector: string): string | null {
  const m = selector.match(CLASS_PREFIX);
  return m ? m[1]!.replace(/\\/g, '') : null;
}

function matches(selector: string, classes: string[], state: ElementState) {
  const m = selector.match(CLASS_PREFIX);
  if (!m) return false;
  const cls = m[1]!.replace(/\\/g, '');
  if (!classes.includes(cls)) return false;
  const rest = selector.slice(m[0].length);
  if (rest.includes(':is(.dark *)') && !state.dark) return false;
  if (rest.includes(':hover') && !state.hover) return false;
  if (rest.includes(':active') && !state.active) return false;
  if (rest.includes('[aria-expanded="true"]') && !state.expanded) return false;
  const unmodelled = rest.replace(/:is\(\.dark \*\)|:hover|:active|\[aria-expanded="true"\]/g, '');
  return unmodelled.trim() === '';
}

function winning(
  rules: FlatRule[],
  classes: string[],
  state: ElementState,
  prop: string,
): string | null {
  const candidates = rules.filter((r) => r.prop === prop && matches(r.sel, classes, state));
  if (candidates.length === 0) return null;
  candidates.sort((a, b) => specificity(a.sel) - specificity(b.sel) || a.order - b.order);
  return baseClassOf(candidates.at(-1)!.sel);
}

const WEB_CSS = resolve(repoRoot, 'apps/web/app/globals.css');
const DESKTOP_CSS = resolve(repoRoot, 'apps/desktop/src/renderer/src/globals.css');

const brand = cn(buttonVariants({ variant: 'brand', size: 'sm' }), 'h-8 px-2');
const brandSubtle = cn(buttonVariants({ variant: 'brandSubtle', size: 'sm' }), 'h-8 px-2');

describe('brand Button variants resolve to brand colour in the real stylesheet', () => {
  let rules: FlatRule[];

  beforeAll(async () => {
    rules = await compileStylesheet(WEB_CSS);
  }, 60_000);

  const bg = (classes: string, state: ElementState) =>
    winning(rules, classes.split(/\s+/), state, 'background-color');
  const text = (classes: string, state: ElementState) =>
    winning(rules, classes.split(/\s+/), state, 'color');

  describe('brand (filter ON — the loud filled tier)', () => {
    for (const dark of [false, true]) {
      const theme = dark ? 'dark' : 'light';

      it(`fills with brand and never with the neutral input token (${theme})`, () => {
        expect(bg(brand, { dark })).toBe('bg-brand');
      });

      it(`deepens one notch on hover, another when pressed (${theme})`, () => {
        expect(bg(brand, { dark, hover: true })).toBe('hover:bg-brand/90');
        expect(bg(brand, { dark, hover: true, active: true })).toBe('active:bg-brand/85');
      });

      it(`reads as hover, not as a colour change, while the popover is open (${theme})`, () => {
        expect(bg(brand, { dark, expanded: true })).toBe('aria-expanded:bg-brand/90');
      });

      it(`keeps brand-foreground text (${theme})`, () => {
        expect(text(brand, { dark })).toBe('text-brand-foreground');
      });
    }
  });

  describe('brandSubtle (activity, filter OFF — the tint tier)', () => {
    it('uses the light notches in light mode', () => {
      expect(bg(brandSubtle, {})).toBe('bg-brand/7');
      expect(bg(brandSubtle, { hover: true })).toBe('hover:bg-brand/12');
      expect(bg(brandSubtle, { hover: true, active: true })).toBe('active:bg-brand/16');
      expect(bg(brandSubtle, { expanded: true })).toBe('aria-expanded:bg-brand/12');
    });

    it('uses the hotter dark notches in dark mode', () => {
      expect(bg(brandSubtle, { dark: true })).toBe('dark:bg-brand/12');
      expect(bg(brandSubtle, { dark: true, hover: true })).toBe('dark:hover:bg-brand/18');
      expect(bg(brandSubtle, { dark: true, hover: true, active: true })).toBe(
        'dark:active:bg-brand/24',
      );
      expect(bg(brandSubtle, { dark: true, expanded: true })).toBe(
        'dark:aria-expanded:bg-brand/18',
      );
    });
  });

  describe('the mistakes this design prevents', () => {
    it('shows why layering brand over `outline` cannot work in dark mode', () => {
      const layered = cn(
        buttonVariants({ variant: 'outline', size: 'sm' }),
        'border-brand bg-brand text-brand-foreground',
      );

      expect(layered).toContain('dark:bg-input/30');
      expect(bg(layered, { dark: true })).toBe('dark:bg-input/30');
      expect(bg(layered, {})).toBe('bg-brand');
    });

    it('shows why a colour class in `className` beats the variant that owns it', () => {
      const overridden = cn(
        buttonVariants({ variant: 'brand', size: 'sm' }),
        'text-muted-foreground',
      );

      expect(text(overridden, {})).toBe('text-muted-foreground');
      expect(text(overridden, { dark: true })).toBe('text-muted-foreground');
    });
  });
});

describe('desktop ships the same brand cascade as web', () => {
  let rules: FlatRule[];

  beforeAll(async () => {
    rules = await compileStylesheet(DESKTOP_CSS);
  }, 60_000);

  it('fills the brand tier with brand in both themes', () => {
    const classes = brand.split(/\s+/);
    expect(winning(rules, classes, {}, 'background-color')).toBe('bg-brand');
    expect(winning(rules, classes, { dark: true }, 'background-color')).toBe('bg-brand');
    expect(winning(rules, classes, { dark: true }, 'color')).toBe('text-brand-foreground');
  });

  it("keeps the tint tier's dark notches", () => {
    const classes = brandSubtle.split(/\s+/);
    expect(winning(rules, classes, { dark: true }, 'background-color')).toBe('dark:bg-brand/12');
  });
});

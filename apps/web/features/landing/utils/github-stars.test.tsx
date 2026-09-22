import type { ReactNode } from 'react';
import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { GithubStarsProvider, formatStarCount, useGithubStars } from './github-stars';
import { parseGithubStarsEnv } from './github-stars-env';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('useGithubStars', () => {
  it('returns null and performs no network request when no value is configured', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useGithubStars(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <GithubStarsProvider stars={null}>{children}</GithubStarsProvider>
      ),
    });

    expect(result.current).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('returns the server-provided value without fetching', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useGithubStars(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <GithubStarsProvider stars={37_600}>{children}</GithubStarsProvider>
      ),
    });

    expect(result.current).toBe(37_600);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('degrades to null outside the provider instead of throwing or fetching', () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useGithubStars());

    expect(result.current).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('parseGithubStarsEnv', () => {
  it('parses plain non-negative integers', () => {
    expect(parseGithubStarsEnv('0')).toBe(0);
    expect(parseGithubStarsEnv('1234')).toBe(1234);
    expect(parseGithubStarsEnv(' 42 ')).toBe(42);
  });

  it('returns null for unset or empty values', () => {
    expect(parseGithubStarsEnv(undefined)).toBeNull();
    expect(parseGithubStarsEnv('')).toBeNull();
    expect(parseGithubStarsEnv('   ')).toBeNull();
  });

  it('returns null for non-numeric or malformed values', () => {
    expect(parseGithubStarsEnv('lots')).toBeNull();
    expect(parseGithubStarsEnv('-5')).toBeNull();
    expect(parseGithubStarsEnv('1.5')).toBeNull();
    expect(parseGithubStarsEnv('1e3')).toBeNull();
  });
});

describe('formatStarCount', () => {
  it('renders counts below 1,000 exactly', () => {
    expect(formatStarCount(0)).toBe('0');
    expect(formatStarCount(7)).toBe('7');
    expect(formatStarCount(999)).toBe('999');
  });

  it('formats thousands with one decimal, GitHub-style', () => {
    expect(formatStarCount(37_600)).toBe('37.6k');
    expect(formatStarCount(1_234)).toBe('1.2k');
    expect(formatStarCount(12_300)).toBe('12.3k');
  });

  it("trims a trailing .0 ('1k', not '1.0k')", () => {
    expect(formatStarCount(1_000)).toBe('1k');
    expect(formatStarCount(2_000)).toBe('2k');
  });

  it('rounds to one decimal like the repo header', () => {
    expect(formatStarCount(1_949)).toBe('1.9k');
    expect(formatStarCount(1_990)).toBe('2k');
  });

  it("formats millions with an 'm' suffix", () => {
    expect(formatStarCount(1_200_000)).toBe('1.2m');
    expect(formatStarCount(2_000_000)).toBe('2m');
  });
});

import { useRef } from 'react';

export function useWorkspaceSeen(slug: string | undefined, resolved: boolean): boolean {
  const seenRef = useRef<Set<string>>(new Set());
  if (resolved && slug) seenRef.current.add(slug);
  return slug ? seenRef.current.has(slug) : false;
}

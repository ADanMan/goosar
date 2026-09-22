import { useSyncExternalStore } from 'react';

export interface BlockedImageLabels {
  ariaLabel: string;
  text: string;
  title: string;
}

const DEFAULT_LABELS: BlockedImageLabels = {
  ariaLabel: 'Blocked external image',
  text: 'External image blocked',
  title: "External image blocked by this deployment's image policy",
};

let currentLabels: BlockedImageLabels = DEFAULT_LABELS;
const listeners = new Set<() => void>();

export function setBlockedImageLabels(labels: Partial<BlockedImageLabels>): void {
  const next: BlockedImageLabels = {
    ariaLabel: labels.ariaLabel ?? DEFAULT_LABELS.ariaLabel,
    text: labels.text ?? DEFAULT_LABELS.text,
    title: labels.title ?? DEFAULT_LABELS.title,
  };
  if (
    next.ariaLabel === currentLabels.ariaLabel &&
    next.text === currentLabels.text &&
    next.title === currentLabels.title
  ) {
    return;
  }
  currentLabels = next;
  for (const listener of listeners) listener();
}

export function resetBlockedImageLabels(): void {
  setBlockedImageLabels(DEFAULT_LABELS);
}

export function getBlockedImageLabels(): BlockedImageLabels {
  return currentLabels;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): BlockedImageLabels {
  return currentLabels;
}

export function useBlockedImageLabels(): BlockedImageLabels {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}

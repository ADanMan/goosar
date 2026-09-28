'use client';

import { useSyncExternalStore } from 'react';

// Общее состояние сворачивания контекстной панели (T-020): и web
// (ContextPanel сама держит кнопку и клавишу `[`), и десктоп (SidebarTrigger
// в WindowToolbar) должны переключать одну и ту же панель. Módуль-синглтон +
// useSyncExternalStore вместо контекста/стора — состояние живёт вне дерева
// React, что и нужно двум независимым потребителям без общего провайдера.
const STORAGE_KEY = 'goosar.contextPanel.collapsed';
const NARROW_WIDTH = 900;
const WIDE_WIDTH = 1200;

function readInitial(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored !== null) return stored === 'true';
  } catch {
    // private mode etc — fall through to the width heuristic below
  }
  // Первое открытие без сохранённого состояния: узкое окно (< 900px)
  // стартует свёрнутым, широкое (>= 1200px) — развёрнутым.
  if (window.innerWidth < NARROW_WIDTH) return true;
  if (window.innerWidth >= WIDE_WIDTH) return false;
  return false;
}

let collapsed = readInitial();
const listeners = new Set<() => void>();

function setCollapsed(next: boolean | ((c: boolean) => boolean)) {
  const value = typeof next === 'function' ? next(collapsed) : next;
  if (value === collapsed) return;
  collapsed = value;
  try {
    window.localStorage.setItem(STORAGE_KEY, String(collapsed));
  } catch {
    // ponytail: localStorage can throw in private mode; collapse state just
    // won't persist, no need to surface an error for that.
  }
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot() {
  return collapsed;
}

function getServerSnapshot() {
  return false;
}

export function useContextPanelState() {
  const value = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  return {
    collapsed: value,
    setCollapsed,
    toggle: () => setCollapsed((c) => !c),
  };
}

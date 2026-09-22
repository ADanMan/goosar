// Тесты реестра управления диагностикой: одно глобальное значение не переживает
// несколько окон, поэтому состояние ведётся per-window.
import { describe, expect, it, vi } from 'vitest';

import { createDiagnosticsControlRegistry } from './diagnostics-control-registry';

function setup() {
  const warm = vi.fn();
  const cool = vi.fn();
  const registry = createDiagnosticsControlRegistry<object>({ warm, cool });
  return { registry, warm, cool, mainWindow: {}, issueWindow: {} };
}

describe('per-renderer control', () => {
  it('warms a newly opened window even though another window is already on', () => {
    const { registry, warm, mainWindow, issueWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: true });
    warm.mockClear();

    registry.apply(issueWindow, { stackCaptureEnabled: false });
    registry.apply(issueWindow, { stackCaptureEnabled: true });

    expect(warm).toHaveBeenCalledTimes(1);
    expect(warm).toHaveBeenCalledWith(issueWindow);
    expect(registry.isStackCaptureEnabled(issueWindow)).toBe(true);
  });

  it("does not let a new window's pre-config default revoke another window", () => {
    const { registry, cool, mainWindow, issueWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: true });

    registry.apply(issueWindow, { stackCaptureEnabled: false });

    expect(cool).not.toHaveBeenCalledWith(mainWindow);
    expect(registry.isStackCaptureEnabled(mainWindow)).toBe(true);
    expect(registry.isStackCaptureEnabled(issueWindow)).toBe(false);
  });

  it('cools only the window that revoked', () => {
    const { registry, cool, mainWindow, issueWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: true });
    registry.apply(issueWindow, { stackCaptureEnabled: true });

    registry.apply(mainWindow, { stackCaptureEnabled: false });

    expect(cool).toHaveBeenCalledTimes(1);
    expect(cool).toHaveBeenCalledWith(mainWindow);
    expect(registry.isStackCaptureEnabled(issueWindow)).toBe(true);
  });

  it('does not re-warm a window that repeats the same value', () => {
    const { registry, warm, mainWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: true });
    registry.apply(mainWindow, { stackCaptureEnabled: true });

    expect(warm).toHaveBeenCalledTimes(1);
  });

  it('does not cool a window that was never warm', () => {
    const { registry, cool, mainWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: false });

    expect(cool).not.toHaveBeenCalled();
  });
});

describe('fail-closed', () => {
  it('starts disabled for a renderer that has never reported', () => {
    const { registry, mainWindow } = setup();
    expect(registry.isStackCaptureEnabled(mainWindow)).toBe(false);
  });

  it.each([
    ['a malformed payload', { stackCaptureEnabled: 'true' }],
    ['null', null],
    ['undefined', undefined],
    ['an empty object', {}],
  ])('treats %s as off', (_label, payload) => {
    const { registry, warm, mainWindow } = setup();
    registry.apply(mainWindow, payload);

    expect(warm).not.toHaveBeenCalled();
    expect(registry.isStackCaptureEnabled(mainWindow)).toBe(false);
  });

  it('revokes when a warm renderer later sends garbage', () => {
    const { registry, cool, mainWindow } = setup();
    registry.apply(mainWindow, { stackCaptureEnabled: true });

    registry.apply(mainWindow, 'nonsense');

    expect(cool).toHaveBeenCalledWith(mainWindow);
    expect(registry.isStackCaptureEnabled(mainWindow)).toBe(false);
  });
});

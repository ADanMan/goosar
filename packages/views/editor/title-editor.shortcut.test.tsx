import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  configureShortcutPlatform,
  createShortcutChord,
  useShortcutStore,
} from '@goosar/core/shortcuts';
import { TitleEditor } from './title-editor';

vi.mock('../i18n', () => ({
  useT: () => ({ t: () => '' }),
}));

function pressEnter(options: KeyboardEventInit = {}) {
  const target = document.querySelector('.title-editor') as HTMLElement;
  target.dispatchEvent(
    new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      cancelable: true,
      ...options,
    }),
  );
}

describe('TitleEditor send chord (real editor)', () => {
  beforeEach(() => {
    configureShortcutPlatform('macos');
    useShortcutStore.setState({ overrides: {} });
  });

  afterEach(() => {
    useShortcutStore.setState({ overrides: {} });
  });

  it('fires onSubmitShortcut on the default Cmd+Enter chord', async () => {
    const onSubmitShortcut = vi.fn();
    render(<TitleEditor defaultValue="A title" onSubmitShortcut={onSubmitShortcut} />);
    await screen.findByText('A title');

    pressEnter({ metaKey: true });

    expect(onSubmitShortcut).toHaveBeenCalledTimes(1);
  });

  it('leaves plain Enter to the keymap, so it never creates', async () => {
    const onSubmitShortcut = vi.fn();
    render(<TitleEditor defaultValue="A title" onSubmitShortcut={onSubmitShortcut} />);
    await screen.findByText('A title');

    pressEnter();

    expect(onSubmitShortcut).not.toHaveBeenCalled();
  });

  it('still refuses plain Enter when the user binds send to Enter', async () => {
    useShortcutStore.setState({ overrides: { send: createShortcutChord('Enter') } });
    const onSubmitShortcut = vi.fn();
    render(<TitleEditor defaultValue="A title" onSubmitShortcut={onSubmitShortcut} />);
    await screen.findByText('A title');

    pressEnter();

    expect(onSubmitShortcut).not.toHaveBeenCalled();
  });

  it('does not fire the chord for hosts that only pass onSubmit', async () => {
    const onSubmit = vi.fn();
    render(<TitleEditor defaultValue="A title" onSubmit={onSubmit} />);
    await screen.findByText('A title');

    pressEnter({ metaKey: true });
    expect(onSubmit).not.toHaveBeenCalled();

    pressEnter();
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});

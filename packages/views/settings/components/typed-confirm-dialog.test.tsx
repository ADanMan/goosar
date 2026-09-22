import type { ReactNode } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { render as rtlRender, screen, fireEvent, type RenderOptions } from '@testing-library/react';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';

const TEST_RESOURCES = {
  en: { common: enCommon, settings: enSettings },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function render(ui: React.ReactElement, options?: RenderOptions) {
  return rtlRender(ui, { wrapper: I18nWrapper, ...options });
}

const mocks = vi.hoisted(() => ({
  dialog: { onOpenChange: undefined as ((open: boolean) => void) | undefined },
}));

vi.mock('@goosar/ui/components/ui/dialog', () => ({
  Dialog: ({
    children,
    onOpenChange,
  }: {
    children: ReactNode;
    onOpenChange?: (open: boolean) => void;
  }) => {
    mocks.dialog.onOpenChange = onOpenChange;
    return <div>{children}</div>;
  },
  DialogContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: ReactNode }) => <h1>{children}</h1>,
  DialogDescription: ({ children }: { children: ReactNode }) => <p>{children}</p>,
  DialogFooter: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

import { TypedConfirmDialog } from './typed-confirm-dialog';

function mount(overrides: Partial<Parameters<typeof TypedConfirmDialog>[0]> = {}) {
  const onConfirm = vi.fn();
  const onClose = vi.fn();
  render(
    <TypedConfirmDialog
      inputId="typed-confirm"
      title="Revoke docx-skill?"
      description="It stops being delivered."
      target="docx-skill"
      unavailableNote="There is nothing to type here."
      confirmLabel="Revoke package"
      cancelLabel="Cancel"
      loading={false}
      onClose={onClose}
      onConfirm={onConfirm}
      {...overrides}
    />,
  );
  return { onConfirm, onClose };
}

describe('TypedConfirmDialog', () => {
  it('does not confirm on the Enter that commits an IME composition', () => {
    const { onConfirm } = mount();
    const input = screen.getByLabelText(/Type/);
    fireEvent.change(input, { target: { value: 'docx-skill' } });

    fireEvent.keyDown(input, { key: 'Enter', keyCode: 229 });
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('fails closed when the target is empty instead of pre-satisfying itself', () => {
    const { onConfirm } = mount({ target: '' });
    expect(screen.queryByLabelText(/Type/)).toBeNull();
    expect(screen.getByText('There is nothing to type here.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Revoke package' }));
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('never spells the answer out in the placeholder', () => {
    mount();
    const input = screen.getByLabelText(/Type/) as HTMLInputElement;
    expect(input.placeholder).toBe('Exact value');
    expect(input.placeholder).not.toContain('docx-skill');
  });

  it('does not close on Escape or a backdrop click while the write is in flight', () => {
    const { onClose } = mount({ loading: true });
    mocks.dialog.onOpenChange?.(false);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('still closes on Escape or a backdrop click before the write starts', () => {
    const { onClose } = mount({ loading: false });
    mocks.dialog.onOpenChange?.(false);
    expect(onClose).toHaveBeenCalled();
  });

  it('freezes the field and both buttons while the write is in flight', () => {
    const { onConfirm, onClose } = mount({ loading: true });
    fireEvent.change(screen.getByLabelText(/Type/), {
      target: { value: 'docx-skill' },
    });
    fireEvent.keyDown(screen.getByLabelText(/Type/), { key: 'Enter' });
    expect(onConfirm).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    expect(onClose).not.toHaveBeenCalled();
  });
});

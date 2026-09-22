// Тесты: компонент — единственный путь блокировки автообновления против сервера
// в закрытом контуре; отсутствующий, поздний или неизвестный профиль публикует
// false (обновления разрешены).
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from '@testing-library/react';

const configState: { deliveryProfile: string } = { deliveryProfile: '' };

vi.mock('@goosar/core/config', () => ({
  useConfigStore: (selector: (s: typeof configState) => unknown) => selector(configState),
  isPerimeterDeliveryProfile: (profile: string | undefined) => profile === 'perimeter',
}));

import { UpdateControlReporter } from './update-control-reporter';

const setUpdateControl = vi.fn();

beforeEach(() => {
  setUpdateControl.mockClear();
  configState.deliveryProfile = '';
  Object.defineProperty(window, 'desktopAPI', {
    configurable: true,
    value: { setUpdateControl },
  });
});

describe('UpdateControlReporter', () => {
  it('publishes cloud before any config has arrived', () => {
    render(<UpdateControlReporter />);

    expect(setUpdateControl).toHaveBeenCalledWith({ perimeterProfile: false });
  });

  it('publishes cloud for unknown future profiles (default branch)', () => {
    configState.deliveryProfile = 'airgap';

    render(<UpdateControlReporter />);

    expect(setUpdateControl).toHaveBeenCalledWith({ perimeterProfile: false });
  });

  it('publishes perimeter when the server advertises it', () => {
    configState.deliveryProfile = 'perimeter';

    render(<UpdateControlReporter />);

    expect(setUpdateControl).toHaveBeenCalledWith({ perimeterProfile: true });
  });

  it('re-publishes when the profile arrives after mount', () => {
    const { rerender } = render(<UpdateControlReporter />);
    expect(setUpdateControl).toHaveBeenCalledWith({ perimeterProfile: false });

    configState.deliveryProfile = 'perimeter';
    rerender(<UpdateControlReporter />);

    expect(setUpdateControl).toHaveBeenLastCalledWith({
      perimeterProfile: true,
    });
  });

  it('does not re-publish an unchanged value', () => {
    const { rerender } = render(<UpdateControlReporter />);
    expect(setUpdateControl).toHaveBeenCalledTimes(1);

    rerender(<UpdateControlReporter />);

    expect(setUpdateControl).toHaveBeenCalledTimes(1);
  });
});

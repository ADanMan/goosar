import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Command, CommandInput } from '@goosar/ui/components/ui/command';

describe('CommandInput', () => {
  const renderInput = () => {
    const ancestorKeyDown = vi.fn();
    const callerKeyDown = vi.fn();
    render(
      <div onKeyDown={ancestorKeyDown}>
        <Command>
          <CommandInput placeholder="search" onKeyDown={callerKeyDown} />
        </Command>
      </div>,
    );
    return { ancestorKeyDown, callerKeyDown };
  };

  it.each(['{Home}', '{End}'])(
    'stops %s from bubbling past the input while still calling the caller onKeyDown',
    async (key) => {
      const user = userEvent.setup();
      const { ancestorKeyDown, callerKeyDown } = renderInput();

      await user.click(screen.getByPlaceholderText('search'));
      await user.keyboard(key);

      expect(callerKeyDown).toHaveBeenCalledTimes(1);
      expect(ancestorKeyDown).not.toHaveBeenCalled();
    },
  );

  it('lets other keys bubble so cmdk list navigation keeps working', async () => {
    const user = userEvent.setup();
    const { ancestorKeyDown, callerKeyDown } = renderInput();

    await user.click(screen.getByPlaceholderText('search'));
    await user.keyboard('{ArrowDown}');

    expect(callerKeyDown).toHaveBeenCalledTimes(1);
    expect(ancestorKeyDown).toHaveBeenCalledTimes(1);
  });
});

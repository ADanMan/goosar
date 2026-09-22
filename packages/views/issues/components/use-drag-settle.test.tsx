/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from 'vitest';
import { useEffect } from 'react';
import { render, act } from '@testing-library/react';
import { useDragSettle } from './use-drag-settle';

describe('useDragSettle', () => {
  it('does not loop when a resync effect rebuilds a content-equal column map every render', () => {
    let renders = 0;
    function Harness() {
      renders++;
      const { columns, setColumns } = useDragSettle(() => ({ todo: ['a', 'b'] }));
      useEffect(() => {
        setColumns({ todo: ['a', 'b'] });
      });
      return <div>{Object.keys(columns).join(',')}</div>;
    }

    expect(() => render(<Harness />)).not.toThrow();
    expect(renders).toBeLessThan(10);
  });

  it('still applies a content-changed column update', () => {
    function Harness({ next }: { next: Record<string, string[]> }) {
      const { columns, setColumns } = useDragSettle(() => ({ todo: ['a'] }));
      useEffect(() => {
        setColumns(next);
      }, [next, setColumns]);
      return <div data-testid="cols">{(columns.todo ?? []).join(',')}</div>;
    }

    const { getByTestId, rerender } = render(<Harness next={{ todo: ['a'] }} />);
    expect(getByTestId('cols').textContent).toBe('a');

    act(() => {
      rerender(<Harness next={{ todo: ['a', 'b'] }} />);
    });
    expect(getByTestId('cols').textContent).toBe('a,b');
  });
});

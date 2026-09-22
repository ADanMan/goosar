'use client';

import { forwardRef } from 'react';
import { useNavigation } from './context';

interface AppLinkProps extends React.AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  newTabTitle?: string;
}

export const AppLink = forwardRef<HTMLAnchorElement, AppLinkProps>(function AppLink(
  { href, children, onClick, onMouseEnter, onFocus, target, newTabTitle, ...props },
  ref,
) {
  const { push, openInNewTab, prefetch } = useNavigation();

  const handleClick = (e: React.MouseEvent<HTMLAnchorElement>) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(href, newTabTitle);
      }
      return;
    }
    if (target === '_blank') {
      onClick?.(e);
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(href, newTabTitle, { activate: true });
      }
      return;
    }
    e.preventDefault();
    onClick?.(e);
    push(href);
  };

  const handleMouseEnter = (e: React.MouseEvent<HTMLAnchorElement>) => {
    prefetch?.(href);
    onMouseEnter?.(e);
  };

  const handleFocus = (e: React.FocusEvent<HTMLAnchorElement>) => {
    prefetch?.(href);
    onFocus?.(e);
  };

  return (
    <a
      ref={ref}
      href={href}
      target={target}
      rel={target === '_blank' ? 'noopener noreferrer' : undefined}
      {...props}
      onClick={handleClick}
      onMouseEnter={handleMouseEnter}
      onFocus={handleFocus}
    >
      {children}
    </a>
  );
});

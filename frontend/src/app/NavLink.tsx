import type { AnchorHTMLAttributes, MouseEvent, ReactNode } from 'react';

/** A real link (so it can be opened in a new tab) that navigates inside the app on a plain click. */
export function NavLink({
  href,
  onNavigate,
  className,
  children,
  ...anchorProps
}: {
  href: string;
  onNavigate: () => void;
  className?: string;
  children: ReactNode;
} & Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href' | 'onClick' | 'children' | 'className'>) {
  function onClick(event: MouseEvent<HTMLAnchorElement>) {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onNavigate();
  }
  return (
    <a {...anchorProps} href={href} onClick={onClick} className={className}>
      {children}
    </a>
  );
}

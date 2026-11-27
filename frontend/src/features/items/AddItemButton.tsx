import type { ButtonHTMLAttributes, Ref } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { PlusIcon } from './HomeIcons';

/** The screen's primary Add item action on the title line, shared by Home and All items so both stay the same. */
export function AddItemButton({ buttonRef, ...props }: { buttonRef?: Ref<HTMLButtonElement> }
  & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'className' | 'type'>) {
  const { t } = useI18n();
  return (
    <Button ref={buttonRef} type="button" className="min-h-11 shrink-0 gap-1.5 px-4" {...props}>
      <PlusIcon className="size-4" />
      {t('items.add')}
    </Button>
  );
}

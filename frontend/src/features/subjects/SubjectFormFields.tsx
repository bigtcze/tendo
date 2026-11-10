import type { RefObject } from 'react';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { cn } from '../../lib/cn';
import { useI18n } from '../../i18n';
import { subjectTypes, type SubjectType } from './subjectsApi';
import { type ErrorTarget, typeLabelKey } from './subjectFormModel';

type FieldProps = {
  initialFocusRef?: RefObject<HTMLInputElement | null>;
  uid: string; nameId: string; errorId: string; target: ErrorTarget; name: string; changeName: (name: string) => void;
  type: SubjectType; changeType: (type: SubjectType) => void; locked: boolean; memberPerson: boolean;
};

export function SubjectFormFields(props: FieldProps) {
  const { t } = useI18n();
  const { initialFocusRef, name, changeName, errorId, target, type, changeType, uid, nameId, locked, memberPerson } = props;
  return <>
    <NameField {...{ name, changeName, errorId, target, locked }} id={nameId} inputRef={initialFocusRef} />
    {memberPerson
      ? <p className="text-sm text-muted">{t('subjects.member.editHint')}</p>
      : <TypeField {...{ uid, type, changeType, target, errorId, locked }} />}
  </>;
}

function NameField({ id, name, changeName, target, errorId, locked, inputRef }: {
  id: string; name: string; changeName: (name: string) => void; target: ErrorTarget;
  errorId: string; locked: boolean; inputRef?: RefObject<HTMLInputElement | null>;
}) {
  const { t } = useI18n();
  return <div className="space-y-2">
    <Label htmlFor={id}>{t('subjects.name')}</Label>
    <Input ref={inputRef} id={id} name="name" type="text" autoComplete="off" required value={name}
      onChange={(event) => changeName(event.target.value)} disabled={locked}
      aria-invalid={target === 'name'} aria-describedby={target === 'name' ? errorId : undefined} />
  </div>;
}

function TypeField({ uid, type, changeType, target, errorId, locked }: {
  uid: string; type: SubjectType; changeType: (type: SubjectType) => void; target: ErrorTarget;
  errorId: string; locked: boolean;
}) {
  const { t } = useI18n();
  return <fieldset role="radiogroup" className="min-w-0 space-y-2" aria-invalid={target === 'type'}
    aria-describedby={target === 'type' ? errorId : undefined}>
    <legend className="text-sm font-semibold text-ink">{t('subjects.type.legend')}</legend>
    <div className="flex flex-wrap gap-2 pt-1">
      {subjectTypes.map((option) => <label key={option} className="relative">
        <input type="radio" name={`${uid}-type`} value={option} checked={type === option}
          onChange={() => changeType(option)} disabled={locked}
          className="peer absolute inset-0 size-full cursor-pointer opacity-0" />
        <span className={cn(
          'inline-flex min-h-11 items-center rounded-xl border border-field bg-raised px-4 text-base text-ink transition-colors',
          'peer-hover:bg-sand peer-checked:border-accent peer-checked:bg-accent peer-checked:text-on-accent',
          'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent',
        )}>{t(typeLabelKey(option))}</span>
      </label>)}
    </div>
  </fieldset>;
}

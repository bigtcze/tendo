import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { cn } from '../../lib/cn';
import { useI18n } from '../../i18n';
import { subjectTypes, type SubjectType } from './subjectsApi';
import {
  hasUnknownMember, shouldShowClearedLinkNotice, shouldShowLinkedAccount, type ErrorTarget,
  type SubjectValues, typeLabelKey,
} from './subjectFormModel';

type FieldProps = {
  uid: string; nameId: string; linkedId: string; errorId: string; target: ErrorTarget; name: string; changeName: (name: string) => void;
  type: SubjectType; changeType: (type: SubjectType) => void; linkedUserId: string | null;
  changeLinkedUserId: (id: string | null) => void; locked: boolean;
  members: { userId: string; login: string }[]; membersLoaded: boolean; initial: SubjectValues;
};

export function SubjectFormFields(props: FieldProps) {
  const { t } = useI18n();
  const { name, changeName, errorId, target, type, changeType, linkedUserId, changeLinkedUserId, uid, nameId, linkedId, locked,
    members, membersLoaded, initial } = props;
  return <>
    <NameField {...{ name, changeName, errorId, target, locked }} id={nameId} />
    <TypeField {...{ uid, type, changeType, target, errorId, locked }} />
    {shouldShowLinkedAccount(type, membersLoaded, members.length, linkedUserId) ? (
      <LinkedAccountField {...{ linkedUserId, changeLinkedUserId, members, locked, target, errorId }} id={linkedId} />
    ) : null}
    {shouldShowClearedLinkNotice(type, initial.linkedUserId, linkedUserId)
      ? <p className="text-sm text-muted">{t('subjects.link.clearedByType')}</p> : null}
  </>;
}

function NameField({ id, name, changeName, target, errorId, locked }: {
  id: string; name: string; changeName: (name: string) => void; target: ErrorTarget;
  errorId: string; locked: boolean;
}) {
  const { t } = useI18n();
  return <div className="space-y-2">
    <Label htmlFor={id}>{t('subjects.name')}</Label>
    <Input id={id} name="name" type="text" autoComplete="off" required value={name}
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
    <legend className="text-sm font-medium text-ink">{t('subjects.type.legend')}</legend>
    <div className="flex flex-wrap gap-2 pt-1">
      {subjectTypes.map((option) => <label key={option} className="relative">
        <input type="radio" name={`${uid}-type`} value={option} checked={type === option}
          onChange={() => changeType(option)} disabled={locked}
          className="peer absolute inset-0 size-full cursor-pointer opacity-0" />
        <span className={cn(
          'inline-flex min-h-11 items-center rounded-xl border border-field bg-white px-4 text-base text-ink transition-colors',
          'peer-hover:bg-sand peer-checked:border-accent peer-checked:bg-accent peer-checked:text-white',
          'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent',
        )}>{t(typeLabelKey(option))}</span>
      </label>)}
    </div>
  </fieldset>;
}

function LinkedAccountField({ id, linkedUserId, changeLinkedUserId, members, locked, target, errorId }: {
  id: string; linkedUserId: string | null; changeLinkedUserId: (id: string | null) => void;
  members: { userId: string; login: string }[]; locked: boolean; target: ErrorTarget;
  errorId: string;
}) {
  const { t } = useI18n();
  const hintId = `${id}-hint`;
  return <div className="space-y-2">
    <Label htmlFor={id} className="text-muted">{t('subjects.link.label')}</Label>
    <select id={id} value={linkedUserId ?? ''}
      onChange={(event) => changeLinkedUserId(event.target.value || null)} disabled={locked}
      aria-invalid={target === 'linkedUserId'} aria-describedby={target === 'linkedUserId' ? errorId : hintId}
      className={cn(
        'min-h-11 w-full rounded-xl border border-field bg-white px-3 text-ink',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      )}>
      <option value="">{t('subjects.link.none')}</option>
      {hasUnknownMember(linkedUserId, members) ? <option value={linkedUserId}>{t('subjects.link.unknown')}</option> : null}
      {members.map((member) => <option key={member.userId} value={member.userId}>{member.login}</option>)}
    </select>
    <p id={hintId} className="text-sm text-muted">{t('subjects.link.hint')}</p>
  </div>;
}

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Subject } from './itemsApi';
import { ItemForm, type ItemFormValues } from './ItemForm';

const subjects: Subject[] = [{ id: 's-1', name: 'Kitchen', type: 'person', linkedUserId: null, archived: false, createdAt: '', updatedAt: '' }];
const members: Member[] = [{ userId: 'u-1', login: 'anna', role: 'owner' }];
const values: ItemFormValues = { title: 'Clean kitchen', subjectId: 's-1', attentionOn: '2026-05-06', notes: '', recurrence: null, responsibleUserId: null };
const renderForm = (props: Partial<React.ComponentProps<typeof ItemForm>> = {}) => render(<I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={vi.fn(async () => ({ kind: 'done' as const }))} onCancel={vi.fn()} {...props} /></I18nProvider>);
const title = () => screen.getByLabelText('What needs doing?');

 describe('ItemForm direct behavior', () => {
  it('submits exact add values, including optional history only for repeating items', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'done' as const }));
    renderForm({ onSubmit });
    await userEvent.type(title(), 'Clean kitchen');
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    await userEvent.clear(screen.getByRole('spinbutton', { name: 'Repeat every' }));
    await userEvent.type(screen.getByRole('spinbutton', { name: 'Repeat every' }), '3');
    await userEvent.click(screen.getByRole('switch', { name: /Count the next repeat/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.type(screen.getByLabelText('Last completed on'), '2026-04-02');
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ title: 'Clean kitchen', subjectId: 's-1', attentionOn: '', notes: '', recurrence: { intervalValue: 3, intervalUnit: 'year', mode: 'after_completion' }, responsibleUserId: null, historicalCompletedOn: '2026-04-02' }));
  });

  it('omits historical date in edit and builds fixed recurrence, or null when Repeat off', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'done' as const }));
    renderForm({ mode: 'edit', initialValues: values, onSubmit });
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(onSubmit).toHaveBeenLastCalledWith({ ...values, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }));
  });

  it('submits locked initial values despite attempted edits and exposes retry label', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'handled' as const }));
    renderForm({ initialValues: values, locked: true, retryLabel: 'Check again', onSubmit });
    expect(screen.getByRole('button', { name: 'Check again' })).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(values));
  });

  it('restores last submitted recurrence and history on unlock, hiding notice', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'handled' as const }));
    const view = renderForm({ preserveDraftOnUnlock: true, onSubmit });
    await userEvent.type(title(), 'Draft');
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.type(screen.getByLabelText('Last completed on'), '2026-04-02');
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    view.rerender(<I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={onSubmit} onCancel={vi.fn()} notice="Retry required" locked preserveDraftOnUnlock /></I18nProvider>);
    view.rerender(<I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={onSubmit} onCancel={vi.fn()} notice="Retry required" preserveDraftOnUnlock /></I18nProvider>);
    expect(screen.queryByText('Retry required')).not.toBeInTheDocument();
    expect(screen.getByRole('switch', { name: 'Repeat' })).toBeChecked();
    expect(screen.getByLabelText('Last completed on')).toHaveValue('2026-04-02');
  });

  it('validates title before subject and focuses/marks the invalid field', async () => {
    renderForm({ subjects: [] });
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter a title.');
    expect(title()).toHaveFocus();
    expect(title()).toHaveAttribute('aria-invalid', 'true');
    expect(title()).toHaveAttribute('aria-describedby');
  });

  it.each(['0', '1000', '1.5'])('validates repeat interval %s and focuses recurrence control', async (interval) => {
    renderForm();
    await userEvent.type(title(), 'Example');
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    const control = screen.getByRole('spinbutton', { name: 'Repeat every' });
    await userEvent.clear(control);
    await userEvent.type(control, interval);
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Choose an interval from 1 to 999.');
    expect(control).toHaveFocus();
    expect(control).toHaveAttribute('aria-invalid', 'true');
  });

  it('opens and focuses notes when the code-point limit is exceeded', async () => {
    renderForm({ initialValues: { ...values, notes: '😀'.repeat(4001) } });
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    const notes = await screen.findByLabelText('Notes');
    expect(notes).toHaveFocus();
    expect(notes).toHaveAttribute('aria-invalid', 'true');
  });

  it('shows responsible chooser for multiple members and retains an unknown selected member', () => {
    renderForm({ members: [...members, { userId: 'u-2', login: 'petr', role: 'member' }], initialValues: { ...values, responsibleUserId: 'u-gone' } });
    expect(screen.getByRole('option', { name: 'Unknown member' })).toHaveValue('u-gone');
  });

  it('focuses notice on mount instead of title', () => {
    renderForm({ notice: 'Try again' });
    expect(screen.getByRole('status')).toHaveFocus();
    expect(title()).not.toHaveFocus();
  });

  it('prevents double submission while request is pending', async () => {
    let resolve!: (value: { kind: 'done' }) => void;
    const onSubmit = vi.fn(() => new Promise<{ kind: 'done' }>((done) => { resolve = done; }));
    renderForm({ onSubmit });
    await userEvent.type(title(), 'Example');
    const submit = screen.getByRole('button', { name: 'Add item' });
    await userEvent.click(submit);
    await userEvent.click(submit);
    expect(onSubmit).toHaveBeenCalledTimes(1);
    resolve({ kind: 'done' });
  });

  it('keeps add Cancel enabled while busy but disables edit controls and Cancel', () => {
    const onCancel = vi.fn();
    const { rerender } = renderForm({ busy: true, onCancel });
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();
    rerender(<I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={vi.fn(async () => ({ kind: 'done' as const }))} onCancel={onCancel} busy mode="edit" initialValues={values} /></I18nProvider>);
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
    expect(title()).toBeDisabled();
  });
  it('submits the latest locked initial values and restores them, recurrence included, on unlock', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'handled' as const }));
    const recovered: ItemFormValues = { ...values, title: 'Recovered', recurrence: { intervalValue: 3, intervalUnit: 'month', mode: 'after_completion' } };
    const form = (props: Partial<React.ComponentProps<typeof ItemForm>>) => <I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={onSubmit} onCancel={vi.fn()} preserveDraftOnUnlock {...props} /></I18nProvider>;
    const view = render(form({ initialValues: values, locked: true, retryLabel: 'Check again' }));
    view.rerender(form({ initialValues: recovered, locked: true, retryLabel: 'Check again' }));
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(recovered));
    view.rerender(form({ initialValues: recovered }));
    expect(title()).toHaveValue('Recovered');
    expect(screen.getByRole('switch', { name: 'Repeat' })).toBeChecked();
    expect(screen.getByRole('spinbutton', { name: 'Repeat every' })).toHaveValue(3);
    expect(screen.getByRole('combobox', { name: 'Repeat interval' })).toHaveValue('month');
    expect(screen.getByRole('switch', { name: /Count the next repeat/ })).toBeChecked();
  });

  it('never sends a historical date from edit mode, even when initial values carry one', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'done' as const }));
    const recurrence = { intervalValue: 1, intervalUnit: 'month' as const, mode: 'fixed' as const };
    renderForm({ mode: 'edit', initialValues: { ...values, recurrence, historicalCompletedOn: '2026-04-02' }, onSubmit });
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ ...values, recurrence }));
  });

  it('ignores a second synchronous submit before the first settles', async () => {
    let resolve!: (value: { kind: 'done' }) => void;
    const onSubmit = vi.fn(() => new Promise<{ kind: 'done' }>((done) => { resolve = done; }));
    renderForm({ onSubmit, initialValues: values });
    fireEvent.submit(screen.getByRole('form'));
    fireEvent.submit(screen.getByRole('form'));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    resolve({ kind: 'done' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add item' })).toBeEnabled());
  });

  it('opens collapsed notes and focuses them for a server notes error', async () => {
    renderForm({ initialValues: values, onSubmit: vi.fn(async () => ({ kind: 'invalid' as const, field: 'notes' as const, code: 'invalid_length' })) });
    expect(screen.queryByLabelText('Notes')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(screen.getByLabelText('Notes')).toHaveFocus());
    expect(screen.getByLabelText('Notes')).toHaveAttribute('aria-invalid', 'true');
  });

  it('opens a collapsed previous-completion date and focuses it for a server error', async () => {
    const recurrence = { intervalValue: 1, intervalUnit: 'month' as const, mode: 'fixed' as const };
    renderForm({ initialValues: { ...values, recurrence }, onSubmit: vi.fn(async () => ({ kind: 'invalid' as const, field: 'historicalCompletedOn' as const, code: 'future_date' })) });
    expect(screen.queryByLabelText('Last completed on')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(screen.getByLabelText('Last completed on')).toHaveFocus());
    expect(screen.getByRole('alert')).toHaveTextContent('A previous completion date cannot be in the future.');
  });
  it.each(['invalid_date', 'toString', 'constructor', '__proto__'])('falls back to the previous-date message for historical code %s', async (code) => {
    const recurrence = { intervalValue: 1, intervalUnit: 'month' as const, mode: 'fixed' as const };
    renderForm({ initialValues: { ...values, recurrence }, onSubmit: vi.fn(async () => ({ kind: 'invalid' as const, field: 'historicalCompletedOn' as const, code })) });
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter a real previous completion date.');
  });

  it('restores the pre-submission baseline, previous date included, when a lock lifts before any submit', async () => {
    const recurrence = { intervalValue: 2, intervalUnit: 'week' as const, mode: 'fixed' as const };
    const initial: ItemFormValues = { ...values, recurrence, historicalCompletedOn: '2026-04-02' };
    const form = (locked: boolean) => <I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={vi.fn(async () => ({ kind: 'done' as const }))} onCancel={vi.fn()} initialValues={initial} preserveDraftOnUnlock locked={locked} /></I18nProvider>;
    const view = render(form(false));
    await userEvent.clear(title());
    await userEvent.type(title(), 'Edited');
    await userEvent.clear(screen.getByLabelText('Last completed on'));
    view.rerender(form(true));
    view.rerender(form(false));
    expect(title()).toHaveValue('Clean kitchen');
    expect(screen.getByLabelText('Last completed on')).toHaveValue('2026-04-02');
    expect(screen.getByRole('spinbutton', { name: 'Repeat every' })).toHaveValue(2);
  });

  it('keeps the current repeat controls on unlock when the submitted values had no recurrence', async () => {
    const onSubmit = vi.fn(async () => ({ kind: 'handled' as const }));
    const form = (locked: boolean) => <I18nProvider><ItemForm subjects={subjects} members={members} onSubmit={onSubmit} onCancel={vi.fn()} preserveDraftOnUnlock locked={locked} /></I18nProvider>;
    const view = render(form(false));
    await userEvent.type(title(), 'One-off');
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ title: 'One-off', recurrence: null })));
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    await userEvent.clear(screen.getByRole('spinbutton', { name: 'Repeat every' }));
    await userEvent.type(screen.getByRole('spinbutton', { name: 'Repeat every' }), '4');
    view.rerender(form(true));
    view.rerender(form(false));
    expect(screen.getByRole('switch', { name: 'Repeat' })).toBeChecked();
    expect(screen.getByRole('spinbutton', { name: 'Repeat every' })).toHaveValue(4);
  });
});

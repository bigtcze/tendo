import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState, type FormEvent } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { Button } from './button';
import { Dialog } from './dialog';
import { Disclosure } from './disclosure';
import { DoneControl } from './list-row';
import { Pagination } from './pagination';
import { Switch } from './switch';
import { Field } from './field';
import { Input } from './input';
import { Select } from './select';
import { Textarea } from './textarea';
import { Skeleton } from './skeleton';
import { EmptyState } from './empty-state';
import { GroupHeader } from './group-header';
import { Notice, NoticeRegion } from './notice';

Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
  configurable: true,
  value(this: HTMLDialogElement) {
    this.open = true;
    this.setAttribute('open', '');
    const initial = this.querySelector<HTMLElement>('[autofocus]') ?? this.querySelector<HTMLElement>('[data-dialog-content] button, [data-dialog-content] input, [data-dialog-content] select, [data-dialog-content] textarea');
    (initial ?? this.querySelector<HTMLElement>('h2'))?.focus();
  },
});
Object.defineProperty(HTMLDialogElement.prototype, 'close', {
  configurable: true,
  value(this: HTMLDialogElement) {
    this.open = false;
    this.removeAttribute('open');
    queueMicrotask(() => this.dispatchEvent(new Event('close')));
  },
});

describe('UI primitive behavior', () => {
  it('dialog uses native modal activation, Escape handling, focus return, and busy dismissal lock', async () => {
    const showModal = vi.spyOn(HTMLDialogElement.prototype, 'showModal');
    const onOpenChange = vi.fn();
    const trigger = document.createElement('button');
    document.body.append(trigger);
    const { rerender, unmount } = render(<Dialog open title="Edit item" closeLabel="Close" onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    expect(showModal).toHaveBeenCalledTimes(1);
    const dialog = screen.getByRole('dialog', { name: 'Edit item' }) as HTMLDialogElement;
    expect(dialog.getAttribute('aria-labelledby')).not.toBe('dialog-title');
    expect(screen.getByRole('button', { name: 'Close' })).toBeVisible();
    expect(dialog.className).toContain('max-w-xl');
    fireEvent(dialog, new Event('cancel', { cancelable: true }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
    rerender(<Dialog open title="Edit item" closeLabel="Close" busy onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    fireEvent(dialog, new Event('cancel', { cancelable: true }));
    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('button', { name: 'Close' })).toBeDisabled();
    const showCallsBeforeUnexpectedClose = showModal.mock.calls.length;
    dialog.close();
    await waitFor(() => expect(dialog.open).toBe(true));
    expect(showModal).toHaveBeenCalledTimes(showCallsBeforeUnexpectedClose + 1);
    rerender(<Dialog open={false} title="Edit item" closeLabel="Close" onOpenChange={onOpenChange} triggerRef={{ current: trigger }}>Form</Dialog>);
    await waitFor(() => expect(trigger).toHaveFocus());
    unmount();
    trigger.remove();
    showModal.mockRestore();
    vi.restoreAllMocks();
  });
  it('dialogs have unique heading ids and sheet classes replace desktop sizing', () => {
    const { container } = render(<><Dialog open title="First" closeLabel="Close first" sheet onOpenChange={() => undefined}>A</Dialog><Dialog open title="Second" closeLabel="Close second" onOpenChange={() => undefined}>B</Dialog></>);
    const dialogs = [...container.querySelectorAll('dialog')];
    const ids = dialogs.map((dialog) => dialog.getAttribute('aria-labelledby'));
    expect(new Set(ids).size).toBe(2);
    expect(dialogs[0].className).toContain('w-full');
    expect(dialogs[0].className).not.toContain('max-w-xl rounded-2xl');
  });
  it('dialog initializes focus to fallback and returns focus only to an external ref', async () => {
    const external = document.createElement('button');
    external.textContent = 'Return';
    document.body.append(external);
    const { rerender, unmount } = render(<Dialog open title="Fallback" closeLabel="Close" returnFocusRef={{ current: external }} onOpenChange={() => undefined}>Body</Dialog>);
    expect(screen.getByRole('heading', { name: 'Fallback' })).toHaveFocus();
    rerender(<Dialog open={false} title="Fallback" closeLabel="Close" returnFocusRef={{ current: external }} onOpenChange={() => undefined}>Body</Dialog>);
    await waitFor(() => expect(external).toHaveFocus());
    unmount();
    external.remove();
  });
  it('dialog ignores stale close events after reopening', async () => {
    const onOpenChange = vi.fn();
    const { rerender } = render(<Dialog open title="Race" closeLabel="Close" onOpenChange={onOpenChange}>Body</Dialog>);
    const dialog = screen.getByRole('dialog', { name: 'Race' }) as HTMLDialogElement;
    rerender(<Dialog open={false} title="Race" closeLabel="Close" onOpenChange={onOpenChange}>Body</Dialog>);
    rerender(<Dialog open title="Race" closeLabel="Close" onOpenChange={onOpenChange}>Body</Dialog>);
    await waitFor(() => expect(dialog.open).toBe(true));
    await new Promise<void>((resolve) => queueMicrotask(resolve));
    await waitFor(() => expect(onOpenChange).not.toHaveBeenCalled());
    expect(dialog.open).toBe(true);
    expect(onOpenChange).not.toHaveBeenCalled();
  });
  it('dialog keeps typed content when changing sheet presentation', () => {
    function DraftDialog({ sheet }: { sheet: boolean }) {
      const [draft, setDraft] = useState('');
      return <Dialog open title="Draft" closeLabel="Close" sheet={sheet} onOpenChange={() => undefined}><input aria-label="Draft text" value={draft} onChange={(event) => setDraft(event.target.value)} /></Dialog>;
    }
    const { rerender } = render(<DraftDialog sheet />);
    fireEvent.change(screen.getByRole('textbox', { name: 'Draft text' }), { target: { value: 'Saved draft' } });
    rerender(<DraftDialog sheet={false} />);
    expect(screen.getByRole('textbox', { name: 'Draft text' })).toHaveValue('Saved draft');
  });
  it('dialog does not reopen after opening has been refused while saving', async () => {
    const { rerender } = render(<Dialog open busy title="Saving" closeLabel="Close" onOpenChange={() => undefined}>Body</Dialog>);
    const dialog = screen.getByRole('dialog', { name: 'Saving' }) as HTMLDialogElement;
    rerender(<Dialog open={false} busy title="Saving" closeLabel="Close" onOpenChange={() => undefined}>Body</Dialog>);
    await waitFor(() => expect(dialog.open).toBe(false));
    expect(dialog).not.toHaveAttribute('open');
  });
  it('button has visible keyboard focus styling and supports disabled and busy states', () => {
    render(<Button disabled aria-busy="true">Save</Button>);
    const button = screen.getByRole('button', { name: 'Save' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('aria-busy', 'true');
    expect(button.className).toContain('focus-visible:outline-focus');
    expect(button).toHaveAttribute('type', 'button');
  });
  it('disclosure wires expanded state and hides/shows its controlled content', () => {
    render(<Disclosure title="More details">Details</Disclosure>);
    const trigger = screen.getByRole('button', { name: 'More details' });
    const content = document.getElementById(trigger.getAttribute('aria-controls')!)!;
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(content).toHaveAttribute('hidden');
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(content).toBeVisible();
    fireEvent.click(trigger);
    expect(content).toHaveAttribute('hidden');
  });
  it('switch exposes checked state and changes on activation', () => {
    const onChange = vi.fn();
    render(<Switch aria-label="Repeat" checked={false} onCheckedChange={onChange} />);
    const toggle = screen.getByRole('switch', { name: 'Repeat' });
    expect(toggle).not.toBeChecked();
    expect(toggle).toHaveAttribute('role', 'switch');
    fireEvent.click(toggle);
    expect(onChange).toHaveBeenCalledWith(true);
  });
  it('switch participates in forms and external labels with native disabled semantics', () => {
    const onCheckedChange = vi.fn();
    const { rerender } = render(<form data-testid="switch-form"><label htmlFor="repeat-toggle">Repeat</label><Switch id="repeat-toggle" name="repeat" checked onCheckedChange={onCheckedChange} /></form>);
    expect(new FormData(screen.getByTestId('switch-form') as HTMLFormElement).get('repeat')).toBe('on');
    fireEvent.click(screen.getByText('Repeat'));
    expect(onCheckedChange).toHaveBeenCalledWith(false);
    rerender(<form data-testid="switch-form"><label htmlFor="repeat-toggle">Repeat</label><Switch id="repeat-toggle" name="repeat" checked={false} onCheckedChange={() => undefined} /></form>);
    expect(new FormData(screen.getByTestId('switch-form') as HTMLFormElement).has('repeat')).toBe(false);
    rerender(<form data-testid="switch-form"><label htmlFor="repeat-toggle">Repeat</label><Switch id="repeat-toggle" name="repeat" checked disabled onCheckedChange={() => undefined} /></form>);
    const disabled = screen.getByRole('switch', { name: 'Repeat' });
    expect(new FormData(screen.getByTestId('switch-form') as HTMLFormElement).has('repeat')).toBe(false);
    fireEvent.click(disabled);
    expect(disabled).toBeChecked();
    expect(onCheckedChange).toHaveBeenCalledTimes(1);
  });
  it('done control has an accessible name, touch target, and pressed state', () => {
    const onToggle = vi.fn();
    const { rerender } = render(<DoneControl label="Mark Clean oven done" done={false} onToggle={onToggle} />);
    const done = screen.getByRole('button', { name: 'Mark Clean oven done' });
    expect(done).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(done);
    expect(onToggle).toHaveBeenCalledOnce();
    rerender(<DoneControl label="Mark Clean oven done" done onToggle={onToggle} />);
    expect(screen.getByRole('button', { name: 'Mark Clean oven done' })).toHaveAttribute('aria-pressed', 'true');
    expect(done.className).toContain('w-11');
    expect(done.className).toContain('h-11');
    rerender(<DoneControl label="Mark Clean oven done" done onToggle={onToggle} />);
    expect(screen.getByRole('button', { name: 'Mark Clean oven done' }).querySelector('[aria-hidden="true"]')).toHaveTextContent('✓');
  });
  it('pagination names cursor controls and range, and disables both bounds', () => {
    const previous = vi.fn();
    const next = vi.fn();
    const { rerender } = render(<Pagination canPrevious canNext range="Items 1–25 of 80" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.getByRole('navigation', { name: 'Item pages' })).toHaveTextContent('Items 1–25 of 80');
    const initialPrevious = screen.getByRole('button', { name: 'Previous' });
    expect(initialPrevious).toBeEnabled();
    fireEvent.click(initialPrevious);
    expect(previous).toHaveBeenCalledOnce();
    previous.mockClear();
    rerender(<Pagination canPrevious={false} canNext range="Items 1–25 of 80" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
    expect(screen.getByRole('navigation', { name: 'Item pages' })).toHaveAccessibleName('Item pages');
    fireEvent.click(screen.getByRole('button', { name: 'Previous' }));
    expect(previous).not.toHaveBeenCalled();
    const nextButton = screen.getByRole('button', { name: 'Next' });
    expect(nextButton).toBeEnabled();
    fireEvent.click(nextButton);
    expect(next).toHaveBeenCalledOnce();
    rerender(<Pagination canPrevious canNext={false} range="Items 26–50 of 80" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
    rerender(<Pagination canPrevious canNext={false} hideNext range="Items 26–50 of 50" label="Item pages" previousLabel="Previous" nextLabel="Next" onPrevious={previous} onNext={next} />);
    expect(screen.queryByRole('button', { name: 'Next' })).not.toBeInTheDocument();
  });
  it('disclosure inside a form does not submit', () => {
    const submit = vi.fn((event: FormEvent<HTMLFormElement>) => event.preventDefault());
    render(<form onSubmit={submit}><Disclosure title="More">Details</Disclosure></form>);
    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    expect(submit).not.toHaveBeenCalled();
  });
  it('field wires label, control, hint and error description', () => {
    render(<Field id="username" label="Username" hint="Used to sign in" error="Required">{(props) => <Input {...props} />}</Field>);
    const input = screen.getByRole('textbox', { name: 'Username' });
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input.getAttribute('aria-describedby')).toContain('username-hint');
    expect(input.getAttribute('aria-describedby')).toContain('username-error');
    expect(input).toHaveAccessibleDescription('Used to sign in Required');
  });
  it('field supports select and textarea controls with invalid description wiring', () => {
    const { rerender } = render(<Field id="choice" label="Choice" error="Choose one">{(props) => <Select {...props}><option>One</option></Select>}</Field>);
    expect(screen.getByRole('combobox')).toHaveAttribute('aria-describedby', 'choice-error');
    rerender(<Field id="notes" label="Notes" error="Required">{(props) => <Textarea {...props} />}</Field>);
    expect(screen.getByRole('textbox', { name: 'Notes' })).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('textbox', { name: 'Notes' })).toHaveAttribute('aria-describedby', 'notes-error');
  });
  it('switch respects disabled state', () => {
    render(<Switch aria-label="Repeat" checked={false} disabled onCheckedChange={() => undefined} />);
    expect(screen.getByRole('switch', { name: 'Repeat' })).toBeDisabled();
  });
  it('skeleton is hidden from assistive technology', () => {
    render(<Skeleton data-testid="loading-skeleton" />);
    expect(screen.getByTestId('loading-skeleton')).toHaveAttribute('aria-hidden', 'true');
  });
  it('empty state has a heading and group header displays count', () => {
    render(<><EmptyState title="Nothing here" /><GroupHeader count={4}>Needs attention</GroupHeader></>);
    expect(screen.getByRole('heading', { name: 'Nothing here' })).toBeVisible();
    expect(screen.getByRole('heading', { name: /^Needs attention$/ })).toBeVisible();
    expect(screen.getByText('4')).toBeVisible();
  });
  it('notice maps tones to alert/status roles', () => {
    const { rerender } = render(<Notice tone="error">Try again</Notice>);
    expect(screen.getByRole('alert')).toHaveTextContent('Try again');
    rerender(<Notice tone="success">Saved</Notice>);
    expect(screen.getByRole('status')).toHaveTextContent('Saved');
  });
  it('notice region keeps one persistent live host mounted while message changes', () => {
    const { container, rerender } = render(<NoticeRegion />);
    const liveRegion = screen.getByRole('status');
    expect(liveRegion).toBeEmptyDOMElement();
    expect(liveRegion).toHaveClass('min-h-0');
    rerender(<NoticeRegion message="Signed in" />);
    expect(screen.getByRole('status')).toBe(liveRegion);
    expect(liveRegion).toHaveTextContent('Signed in');
    const liveOwners = container.querySelectorAll('[role="status"], [role="alert"], [aria-live]');
    expect(liveOwners).toHaveLength(1);
    expect(liveRegion.firstElementChild).not.toHaveAttribute('role');
    rerender(<NoticeRegion tone="error" message="Wrong password" />);
    expect(screen.getByRole('alert')).toHaveTextContent('Wrong password');
    expect(container.querySelectorAll('[role="status"], [role="alert"], [aria-live]')).toHaveLength(1);
  });
  it('dialog syncs controlled state after a genuine native close while idle', async () => {
    const onOpenChange = vi.fn();
    render(<Dialog open title="Native" closeLabel="Close" onOpenChange={onOpenChange}>Body</Dialog>);
    const dialog = screen.getByRole('dialog', { name: 'Native' }) as HTMLDialogElement;
    dialog.close();
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onOpenChange).toHaveBeenCalledTimes(1);
    expect(dialog.open).toBe(false);
  });
  it('switch native input spans the full visible track at touch height', () => {
    render(<Switch aria-label="Repeat" checked={false} onCheckedChange={() => undefined} />);
    const toggle = screen.getByRole('switch', { name: 'Repeat' });
    const root = toggle.closest('[data-switch-root]');
    expect(toggle.className).toContain('inset-0');
    expect(toggle.className).toContain('w-full');
    expect(root).toHaveClass('w-14', 'min-h-11');
    expect(root?.querySelector('[aria-hidden="true"]')).toHaveClass('w-14');
  });
});

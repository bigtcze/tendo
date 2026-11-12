# V3 item form: add and edit (V3-10, packet 1)

> Historical record. It describes the V3 stage (released as `v3.0.0`) and is not the current UX or security contract. Current contracts: [UX](../../.autonomous/UX.md), [SECURITY](../../.autonomous/SECURITY.md).

## Decisions

| Area | Decision | Reason |
|---|---|---|
| Repeat default | Repeat is off; when enabled, the interval begins at one year. | V3-05 row 8 flags one year as the current default and requires a deliberate choice that preserves journey assumptions. Yearly home maintenance is a common case; it is only a starting point, and the unit can be changed. |
| Add presentation | Native Dialog primitive: full-screen sheet below `md`, centered dialog at `md` and wider. | Meets the one-mounted-form contract and reuses the existing inert/Escape/focus behavior. |
| Edit presentation | Same sheet/dialog presentation on phone and larger screens. | Direction review line 88 explicitly selects the same pattern for Edit; keeping conflict summary and rebased form in one modal preserves the comparison context. |
| Form hierarchy | Title, person/thing, optional date, Repeat, recurrence details, then quiet responsible person and notes. | Keeps the one-off route short and visually prioritizes the item title. |
| Repeat explanation | Separate after-completion switch; one household example changes with interval and mode. | Explains the actual consequence with a concise familiar example rather than exposing internal recurrence terminology. |

## Form anatomy and behavior

Title is the first and strongest field, with a concrete short example in the empty input to help a first-time user, followed by person/thing and an optional date. The date hint says it can be left empty to start now. Repeat is a normal off-by-default switch. When enabled, interval and unit appear before the separate after-completion switch. One contextual sentence updates for each mode: fixed dates remain planned if completion is late; after-completion gives a familiar example of the next check being due one selected interval after completion. Add-only previous-completion date is disclosed separately. Responsible and notes remain secondary.

The Add sheet/dialog and Edit sheet/dialog use the shared native `Dialog`; only CSS changes at the breakpoint, not the mounted form. Focus return goes to the originating Add/Edit control. The Dialog has an explicit initial-focus target contract: a state-specific target (uncertain-create recovery notice, otherwise the title input) takes precedence over browser autofocus or generic first-control focus; the fallback heading is for dialogs without a focusable target. This is necessary when the app mounts directly into pending-create recovery. Escape and Cancel work while idle; busy create/edit states cannot dismiss. Catalogue loading/error/retry, no-subject prompt and uncertain create recovery remain available within the Add dialog. Edit conflict summary remains with the editable/rebased draft.

Validation retains focus movement, `aria-invalid`, error descriptions and retry-after-correction. An explicit dialog initial-focus target is respected only during opening; once the user moves focus, later form mounting cannot pull it back. Repeat off continues to submit `recurrence: null`; API and idempotency behavior is unchanged.

## Repeat copy

| State | English | Czech |
|---|---|---|
| After-completion switch | Count the next repeat from when I complete this | Další opakování počítat od dokončení |
| Fixed | A boiler check keeps its planned dates, even if you finish late. | Kontrola kotle zůstává podle plánu, i když ji dokončíte později. |
| After completion | For example, the next boiler check is due {interval} after you finish. | Například další kontrola kotle bude {interval} po dokončení. |

The after-completion sentence uses the selected interval with a singular unit word, e.g. “1 month after you finish”; the fixed sentence stays independent of pluralization. No internal jargon is shown in this form copy.

## Screenshot critique and evidence

The production 320px views fit the sheet without horizontal clipping; the example in the empty title input clarifies the expected kind of item without competing with its label. Czech adds taller but readable switch labels and examples, and the 320px sheet remains scrollable. At 390px the dialog hierarchy is clear; at 1440px the panel is horizontally and vertically centered over the dimmed page. It remains a compact, readable measure rather than stretching across desktop. In edit, the contextual repeat hint and example fit within the panel; the 390px conflict summary remains above the rebased form and is visually distinct without obscuring actions. After-completion Edit captures at 320/360/390 show the switch label wrapping naturally without clipping, and the 768/1024/1440 views keep the compact dialog centered with the dimmed page visible; the recurrence example remains adjacent to the controls. Validation leaves the red outline on the title and the recovery message immediately before the actions. The form is intentionally more compact than the old inline card, but at 320px the one-off controls and submit are visible within the 900px viewport, with little excess vertical space. At shorter phone heights or zoom, the sheet’s form area scrolls; no clipping or horizontal overflow appeared in the checked 320px states.

Production viewport captures and verified PNG dimensions are indexed in [`v3-item-form/CAPTURES.md`](v3-item-form/CAPTURES.md). These are scripted visual evidence, not usability testing or human approval.

## States

- Empty Repeat: one-off item, no recurrence controls, `recurrence: null`.
- Repeat fixed / after-completion: interval, unit, separate switch and mode-specific household example.
- Validation: invalid field is focused and described; draft remains available for correction.
- Add catalogue: loading, retryable error and no-subject prompt.
- Uncertain create: locked form, uncertainty notice and “Check again” recovery.
- Edit conflict: changed latest values remain summarized beside the editable/rebased draft.
- Dark: touched form components use design-system surface, text, line and danger roles. Captures are readiness checks, not full theme sign-off.

## Not done here

- Detail page composition, read view, workflow actions, Done/Undo and history belong to packet 2.
- No full 200/400% zoom review or independent human design review is claimed.
- No real household usability testing is claimed.

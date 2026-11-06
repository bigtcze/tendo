# Authentication and first-run capture index

## Baseline captures

Copied from the authentic V3-02 production audit committed before this change (`a9159e3`, staged synthetic households; no human usability testing):

| File | Route/state | Viewport/locale | Critique |
|---|---|---|---|
| `before/onboarding-en-390.png` | First-run setup | 390/en | The household-name prompt and small technical labels use competing styles; timezone is presented as a raw IANA identifier. |
| `before/onboarding-en-1440.png` | First-run setup | 1440/en | The narrow form uses little of the desktop measure. |
| `before/audit-login-en-390.png` | Sign-in default | 390/en | Password-first behavior is clear, but the screen has little identity beyond a bare form. |
| `before/audit-login-error-en-390.png` | Wrong credentials | 390/en | Both fields appear invalid even though the server cannot identify which value was wrong. |
| `before/audit-login-en-1440.png` | Sign-in default | 1440/en | A narrow form is placed high on a largely empty desktop page. |
| `before/audit-account-en-390.png` | Account | 390/en | The page is sparse and language only points to the distant footer. |

No after captures are committed. The opt-in capture run started but did not finish before its 30-minute timeout; do not treat its partial private output as a complete evidence set.

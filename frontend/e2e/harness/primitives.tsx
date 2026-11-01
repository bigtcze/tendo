import { StrictMode, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './harness.css';
import { Dialog } from '../../src/components/ui/dialog';
import { Switch } from '../../src/components/ui/switch';

function Harness() {
  const [dialogOpen, setDialogOpen] = useState(!location.search.includes('closed'));
  const [dialogBusy] = useState(location.search.includes('busy'));
  const [switchChecked, setSwitchChecked] = useState(false);

  return (
    <main className="space-y-8 p-8">
      <section aria-label="Controlled dialog">
        <p data-testid="dialog-state">{dialogOpen ? 'open' : 'closed'}</p>
        <button type="button" onClick={() => setDialogOpen(true)}>Reopen dialog</button>
        <Dialog
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          title="Primitive dialog"
          closeLabel="Close dialog"
          busy={dialogBusy}
        >
          <form method="dialog">
            <button type="submit">Native close</button>
          </form>
        </Dialog>
      </section>
      <section aria-label="Switch">
        <Switch aria-label="Primitive switch" checked={switchChecked} onCheckedChange={setSwitchChecked} />
      </section>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(<StrictMode><Harness /></StrictMode>);

import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type AiAppsSettings = components['schemas']['McpInstallationPolicy'];
export type AiAppsAccess = components['schemas']['McpScopeList'][number];

const path = '/api/v1/admin/mcp-policy';

/** Refusals every installation read and write can answer; only the grant code makes a 403 the calm refusal. */
type Refusal = { kind: 'unauthenticated' | 'forbidden' | 'rejected' };

function problemCode(error: unknown): string | undefined {
  return (error as { code?: string } | undefined)?.code;
}

function refusal(status: number, code: string | undefined): Refusal | null {
  if (status === 401) return { kind: 'unauthenticated' };
  if (status === 403) return code === 'instance_admin_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
  return null;
}

export type AiAppsRead =
  | { kind: 'ok'; settings: AiAppsSettings; version: string }
  | Refusal
  | { kind: 'error' };

/** The installation's AI app settings and the version a save has to name. A 200 without its version is unusable. */
export async function readAiAppsSettings(): Promise<AiAppsRead> {
  try {
    const { data, error, response } = await api.GET(path);
    const version = response.headers.get('ETag');
    if (data && response.status === 200 && version) return { kind: 'ok', settings: data, version };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    // 503 is the server's own fault and worth retrying; any other answer is a refusal a retry would only repeat.
    if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

export type AiAppsSave =
  | { kind: 'ok'; settings: AiAppsSettings; version: string }
  /** Someone saved in between: nothing was changed and the screen has to read the settings again. */
  | { kind: 'stale' }
  /** Saved, but the answer lacked the version; the screen reads the settings again before another save. */
  | { kind: 'savedUnversioned' }
  /** The server refused the default access itself; the field says what to fix. */
  | { kind: 'invalidAccess' }
  | Refusal
  | { kind: 'error' };

export type AiAppsDraft = { enabled: boolean; defaultAccountEnabled: boolean; defaultMaxScopes: AiAppsAccess[] };

/** Replaces the installation's settings, naming the version they were read at. */
export async function saveAiAppsSettings(draft: AiAppsDraft, version: string): Promise<AiAppsSave> {
  try {
    const { data, error, response } = await api.PUT(path, {
      params: {
        // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
        header: { Origin: window.location.origin, 'If-Match': version },
      },
      body: draft,
    });
    const next = response.headers.get('ETag');
    if (data && response.status === 200 && next) return { kind: 'ok', settings: data, version: next };
    // Saved, but without the version the next save needs: reading again shows what is stored and fetches it.
    if (response.status === 200) return { kind: 'savedUnversioned' };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status === 412) return { kind: 'stale' };
    if (response.status === 422) return { kind: 'invalidAccess' };
    if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type AiAppsSettings = components['schemas']['McpInstallationPolicy'];
export type AiAppsAccess = components['schemas']['McpScopeList'][number];
export type AccountAiApps = components['schemas']['McpAccountPolicy'];
export type AccountAiAppsUpdate = components['schemas']['McpAccountPolicyUpdate'];
export type AiAppsDraft = { enabled: boolean; defaultAccountEnabled: boolean; defaultMaxScopes: AiAppsAccess[] };
export type OwnAiApps = components['schemas']['McpSettings'];
export type OwnAiAppsUpdate = components['schemas']['McpSettingsUpdate'];

const installationPath = '/api/v1/admin/mcp-policy';
const accountPath = '/api/v1/admin/accounts/{userId}/mcp-policy';
const ownPath = '/api/v1/mcp/settings';

/**
 * Refusals every AI apps read and write can answer; only the grant code makes a 403 the calm refusal. `notFound` is
 * only ever the account a per-account route names: the installation settings have no such answer.
 */
type Refusal = { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' };

export type VersionedRead<T> =
  | { kind: 'ok'; value: T; version: string }
  | Refusal
  | { kind: 'error' };

export type VersionedSave<T> =
  | { kind: 'ok'; value: T; version: string }
  /** Someone saved in between: nothing was changed and the screen has to read the settings again. */
  | { kind: 'stale' }
  /** Saved, but the answer lacked the version; the screen reads the settings again before another save. */
  | { kind: 'savedUnversioned' }
  /** The server refused the access value itself; the field says what to fix. */
  | { kind: 'invalidAccess' }
  /** Too many saves in a minute; nothing was changed. */
  | { kind: 'rateLimited' }
  | Refusal
  | { kind: 'error' };

type Answer<T> = { data?: T; error?: unknown; response: Response };

function refusal(status: number, error: unknown, account: boolean): Refusal | null {
  if (status === 401) return { kind: 'unauthenticated' };
  if (status === 403) {
    return (error as { code?: string } | undefined)?.code === 'instance_admin_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
  }
  if (status === 404 && account) return { kind: 'notFound' };
  return null;
}

/** A 200 without its version is unusable: a save naming no version could only be refused or be blind. */
function readAnswer<T>({ data, error, response }: Answer<T>, account: boolean): VersionedRead<T> {
  const version = response.headers.get('ETag');
  if (data && response.status === 200 && version) return { kind: 'ok', value: data, version };
  const refused = refusal(response.status, error, account);
  if (refused) return refused;
  // 503 is the server's own fault and worth retrying; any other answer is a refusal a retry would only repeat.
  if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  return { kind: 'error' };
}

function saveAnswer<T>({ data, error, response }: Answer<T>, account: boolean): VersionedSave<T> {
  const version = response.headers.get('ETag');
  if (data && response.status === 200 && version) return { kind: 'ok', value: data, version };
  // Saved, but without the version the next save needs: reading again shows what is stored and fetches it.
  if (response.status === 200) return { kind: 'savedUnversioned' };
  const refused = refusal(response.status, error, account);
  if (refused) return refused;
  if (response.status === 412) return { kind: 'stale' };
  if (response.status === 422) return { kind: 'invalidAccess' };
  if (response.status === 429) return { kind: 'rateLimited' };
  if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  return { kind: 'error' };
}

/** A request that never got an answer is the common unavailable state. */
async function answered<R>(call: () => Promise<R>): Promise<R | { kind: 'error' }> {
  try {
    return await call();
  } catch {
    return { kind: 'error' };
  }
}

// Browsers set Origin themselves and ignore the value below; it only satisfies the generated type.
const writeHeaders = (version: string) => ({ Origin: window.location.origin, 'If-Match': version });

/** The installation's AI app settings and the version a save has to name. */
export function readAiAppsSettings(): Promise<VersionedRead<AiAppsSettings>> {
  return answered(async () => readAnswer(await api.GET(installationPath), false));
}

/** Replaces the installation's settings, naming the version they were read at. */
export function saveAiAppsSettings(draft: AiAppsDraft, version: string): Promise<VersionedSave<AiAppsSettings>> {
  return answered(async () => saveAnswer(
    await api.PUT(installationPath, { params: { header: writeHeaders(version) }, body: draft }),
    false,
  ));
}

/** One account's AI apps: the administrator's choice, the owner's own and what apps may do now. */
export function readAccountAiApps(userId: string): Promise<VersionedRead<AccountAiApps>> {
  return answered(async () => readAnswer(await api.GET(accountPath, { params: { path: { userId } } }), true));
}

/** Replaces the administrator's choice for one account; the owner's own choice is never changed here. */
export function saveAccountAiApps(userId: string, update: AccountAiAppsUpdate, version: string): Promise<VersionedSave<AccountAiApps>> {
  return answered(async () => saveAnswer(
    await api.PUT(accountPath, { params: { path: { userId }, header: writeHeaders(version) }, body: update }),
    true,
  ));
}

/** The signed-in person's own AI apps: their switch and choice, what they are allowed and what apps may do now. */
export function readOwnAiApps(): Promise<VersionedRead<OwnAiApps>> {
  return answered(async () => readAnswer(await api.GET(ownPath), false));
}

/** Replaces the person's own switch and choice, naming the version they were read at. */
export function saveOwnAiApps(update: OwnAiAppsUpdate, version: string): Promise<VersionedSave<OwnAiApps>> {
  return answered(async () => saveAnswer(
    await api.PUT(ownPath, { params: { header: writeHeaders(version) }, body: update }),
    false,
  ));
}

export type AiAppsChange = components['schemas']['McpPolicyEvent'];

/** `forbidden` is only the installation grant; any other failure is worth a retry, and nothing else is refused here. */
export type AiAppsChangesPage =
  | { kind: 'ok'; items: AiAppsChange[]; nextCursor: string | null }
  | { kind: 'unauthenticated' | 'forbidden' | 'unavailable' };

const CHANGES_PAGE_SIZE = 20;

/** One newest-first page of every change to AI apps: the installation's, one account's and people's own. */
export async function listAiAppsChanges(cursor?: string): Promise<AiAppsChangesPage> {
  try {
    const { data, error, response } = await api.GET('/api/v1/admin/mcp-policy-events', {
      params: { query: { limit: CHANGES_PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor ?? null };
    const refused = refusal(response.status, error, false);
    if (refused?.kind === 'unauthenticated' || refused?.kind === 'forbidden') return { kind: refused.kind };
  } catch { /* the retryable unavailable state */ }
  return { kind: 'unavailable' };
}

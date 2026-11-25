import { InstallationAccountAiAppsScreen } from '../features/installation/InstallationAccountAiAppsScreen';
import { InstallationAccountsScreen } from '../features/installation/InstallationAccountsScreen';
import { InstallationAiAppsScreen } from '../features/installation/InstallationAiAppsScreen';
import type { Route } from './routes';
import type { useAppNavigation } from './useAppNavigation';

type Shared = { login: string; onSignedOut: () => void; onForbidden: () => void };

/**
 * The installation sections about accounts rather than households: the account directory, the installation's AI
 * apps and one account's AI apps. They need nothing from a household and nothing the shell hands between screens.
 */
export function renderAccountSections(route: Route, shared: Shared, viewerId: string, navigation: ReturnType<typeof useAppNavigation>) {
  // The account directory: every account of the installation, and the only place one is deleted for good.
  if (route.kind === 'adminAccounts') {
    return <InstallationAccountsScreen {...shared} viewerId={viewerId} onOpenAiApps={navigation.openAdminAccountAiApps} />;
  }
  if (route.kind === 'adminAiApps') return <InstallationAiAppsScreen {...shared} onOpenAccounts={navigation.openAdminAccounts} />;
  if (route.kind === 'adminAccountAiApps') {
    return <InstallationAccountAiAppsScreen key={route.userId} {...shared} userId={route.userId}
      accountLogin={navigation.adminTargetNameFor(route.userId)} onBack={navigation.openAdminAccounts} />;
  }
  return null;
}

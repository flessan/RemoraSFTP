import { useEffect, useMemo, useState } from 'react';
import { useStore } from './state/store';
import { I18nProvider } from './i18n/i18n';
import { EngineScreen } from './views/EngineScreen';
import { Onboarding } from './views/Onboarding';
import { Sidebar } from './components/Sidebar';
import { TopBar } from './components/TopBar';
import { FileManager } from './fm/FileManager';
import { TransfersView } from './views/TransfersView';
import { ActivityView } from './views/ActivityView';
import { SecurityView } from './views/SecurityView';
import { SettingsView } from './views/SettingsView';
import { ConnectionsView } from './views/ConnectionsView';
import { HostKeyDialog } from './components/HostKeyDialog';
import { Toasts } from './components/Toasts';
import { CommandPalette } from './components/CommandPalette';
import { ConnectionDialog } from './components/ConnectionDialog';

export type View = 'files' | 'transfers' | 'activity' | 'security' | 'settings' | 'connections';

export function App() {
  const store = useStore();
  const [view, setView] = useState<View>('files');
  const [editingConnection, setEditingConnection] = useState<string | null>(null);
  const [creatingConnection, setCreatingConnection] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

  const lang = store.settings?.language ?? 'en';

  // Apply theme.
  useEffect(() => {
    const apply = () => {
      let theme = store.settings?.theme ?? 'system';
      if (theme === 'system') {
        theme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
      }
      document.documentElement.dataset.theme = theme;
    };
    apply();
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', apply);
    return () => mq.removeEventListener('change', apply);
  }, [store.settings?.theme]);

  // Reduced-motion override.
  useEffect(() => {
    if (store.settings?.reducedMotion) {
      document.documentElement.style.setProperty('--reduce-motion', '1');
    }
  }, [store.settings?.reducedMotion]);

  // Global keyboard shortcuts.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const meta = e.ctrlKey || e.metaKey;
      if (meta && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
      if (e.key === 'Escape') setPaletteOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const showOnboarding =
    store.engine === 'ready' && store.authenticated && !store.onboarded &&
    store.connections.length === 0;

  if (store.engine !== 'ready' || !store.authenticated) {
    return (
      <I18nProvider lang={lang}>
        <EngineScreen state={store.engine} />
      </I18nProvider>
    );
  }

  return (
    <I18nProvider lang={lang}>
      <div className="app">
        <TopBar
          view={view}
          onNavigate={setView}
          onNewConnection={() => setCreatingConnection(true)}
          onPalette={() => setPaletteOpen(true)}
        />
        <Sidebar
          view={view}
          onNavigate={setView}
          onNewConnection={() => setCreatingConnection(true)}
        />
        <main className="main" role="main">
          {showOnboarding && <Onboarding onCreate={() => setCreatingConnection(true)} />}
          {!showOnboarding && view === 'files' && <FileManager onNavigate={(v) => setView(v as View)} />}
          {!showOnboarding && view === 'connections' && (
            <ConnectionsView onEdit={(id) => setEditingConnection(id)} onCreate={() => setCreatingConnection(true)} />
          )}
          {!showOnboarding && view === 'transfers' && <TransfersView />}
          {!showOnboarding && view === 'activity' && <ActivityView />}
          {!showOnboarding && view === 'security' && <SecurityView />}
          {!showOnboarding && view === 'settings' && <SettingsView />}
        </main>

        {creatingConnection && (
          <ConnectionDialog
            connection={null}
            onClose={() => setCreatingConnection(false)}
            onSaved={() => {
              setCreatingConnection(false);
              void store.refreshConnections();
            }}
          />
        )}
        {editingConnection && (
          <ConnectionDialog
            connection={store.connections.find((c) => c.id === editingConnection) ?? null}
            onClose={() => setEditingConnection(null)}
            onSaved={() => {
              setEditingConnection(null);
              void store.refreshConnections();
            }}
          />
        )}

        <HostKeyDialog />
        <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} onNavigate={setView} />
        <Toasts />
      </div>
    </I18nProvider>
  );
}

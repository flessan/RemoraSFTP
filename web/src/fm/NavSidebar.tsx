import { useCallback, useEffect, useState } from 'react';
import type { Connection, Entry, Favorite, RecentFileItem, SessionInfo } from '../api/client';
import { api } from '../api/client';
import { useI18n } from '../i18n/i18n';
import { Icon } from '../components/Icons';
import { baseName } from '../util/format';

interface TreeState {
  expanded: Set<string>;
  children: Map<string, Entry[]>;
  loading: Set<string>;
  error: Map<string, boolean>;
}

const emptyTree: TreeState = { expanded: new Set(), children: new Map(), loading: new Set(), error: new Map() };

export function NavSidebar({
  connections,
  sessions,
  activeSession,
  currentPath,
  favorites,
  recentsFiles,
  recentsDirs,
  onHome,
  onNavigate,
  onOpenFavorite,
  onOpenRecent,
  onRemoveFavorite,
  onConnect,
  onDisconnect,
}: {
  connections: Connection[];
  sessions: SessionInfo[];
  activeSession: SessionInfo | null;
  currentPath: string;
  favorites: Favorite[];
  recentsFiles: RecentFileItem[];
  recentsDirs: { connectionId: string; path: string; visitedAt: string }[];
  onHome: () => void;
  onNavigate: (path: string) => void;
  onOpenFavorite: (f: Favorite) => void;
  onOpenRecent: (r: { connectionId: string; path: string; kind: 'file' | 'dir' }) => void;
  onRemoveFavorite: (id: string) => void;
  onConnect: (connId: string) => void;
  onDisconnect: () => void;
}) {
  const { t } = useI18n();
  const [tree, setTree] = useState<TreeState>(emptyTree);
  const [showRecents, setShowRecents] = useState(false);
  const [showFavorites, setShowFavorites] = useState(true);

  const connName = useCallback(
    (id: string) => connections.find((c) => c.id === id)?.name ?? id,
    [connections],
  );

  // Reset the tree when the active connection changes.
  useEffect(() => {
    setTree(emptyTree);
  }, [activeSession?.connectionId]);

  const loadChildren = useCallback(
    async (p: string) => {
      if (!activeSession) return;
      setTree((s) => ({ ...s, loading: new Set(s.loading).add(p) }));
      try {
        const r = await api.list(activeSession.id, p);
        const dirs = (r.entries ?? []).filter((e) => e.type === 'dir' && !e.isSymlink);
        setTree((s) => ({
          ...s,
          children: new Map(s.children).set(p, dirs),
          loading: new Set([...s.loading].filter((x) => x !== p)),
        }));
      } catch {
        setTree((s) => ({
          ...s,
          loading: new Set([...s.loading].filter((x) => x !== p)),
          error: new Map(s.error).set(p, true),
        }));
      }
    },
    [activeSession],
  );

  const toggle = useCallback(
    (e: Entry) => {
      setTree((s) => {
        const expanded = new Set(s.expanded);
        if (expanded.has(e.path)) {
          expanded.delete(e.path);
        } else {
          expanded.add(e.path);
          if (!s.children.has(e.path)) void loadChildren(e.path);
        }
        return { ...s, expanded };
      });
    },
    [loadChildren],
  );

  const renderDir = (e: Entry, depth: number): React.ReactNode => {
    const isExpanded = tree.expanded.has(e.path);
    const kids = tree.children.get(e.path) ?? [];
    const isLoading = tree.loading.has(e.path);
    const isError = tree.error.get(e.path);
    return (
      <div key={e.path}>
        <div
          className={`nav-tree-row ${currentPath === e.path ? 'active' : ''}`}
          style={{ paddingLeft: 8 + depth * 14 }}
          role="treeitem"
          aria-expanded={isExpanded}
          aria-selected={currentPath === e.path}
        >
          <button
            className="btn icon ghost tree-toggle"
            aria-label={isExpanded ? t('fm.treeCollapse') : t('fm.treeExpand')}
            onClick={(ev) => {
              ev.stopPropagation();
              toggle(e);
            }}
          >
            {isLoading ? (
              <Icon.Refresh size={12} />
            ) : isExpanded ? (
              <Icon.ChevronDown size={12} />
            ) : (
              <Icon.ChevronRight size={12} />
            )}
          </button>
          <button
            className="nav-tree-name"
            onClick={() => {
              onNavigate(e.path);
              if (!isExpanded) toggle(e);
            }}
            title={e.path}
          >
            <Icon.Folder size={14} />
            <span>{e.name}</span>
            {isError && <span className="muted" style={{ fontSize: 11 }}>…</span>}
          </button>
        </div>
        {isExpanded && kids.map((k) => renderDir(k, depth + 1))}
        {isExpanded && isError && kids.length === 0 && (
          <div className="muted" style={{ paddingLeft: 22 + depth * 14, fontSize: 11.5 }}>
            {t('fm.treeLoadError')}
          </div>
        )}
      </div>
    );
  };

  const rootDirs = tree.children.get('/') ?? [];
  const hasActive = !!activeSession;

  return (
    <aside className="fm-sidebar" aria-label={t('fm.sidebar')}>
      <button className={`nav-section-item ${!activeSession && currentPath === '/' ? 'active' : ''} ${hasActive ? '' : ''}`} onClick={onHome} disabled={!hasActive}>
        <Icon.Home size={15} />
        <span>{t('fm.navHome')}</span>
        {activeSession && <span className="muted" style={{ fontSize: 11 }}>{activeSession.connectionName}</span>}
      </button>

      <button className="nav-section-head" onClick={() => setShowFavorites(!showFavorites)} aria-expanded={showFavorites}>
        <Icon.Star size={13} />
        <span>{t('fm.navFavorites')}</span>
        <Icon.ChevronRight size={12} className={`chev ${showFavorites ? 'open' : ''}`} />
      </button>
      {showFavorites && (
        <div className="nav-section-body">
          {favorites.length === 0 && <div className="nav-empty">{t('fm.favoritesEmpty')}</div>}
          {favorites.map((f) => (
            <div key={f.id} className="nav-section-row">
              <button
                className={`nav-section-item ${activeSession?.connectionId === f.connectionId && currentPath === f.path ? 'active' : ''}`}
                onClick={() => onOpenFavorite(f)}
                title={`${connName(f.connectionId)}: ${f.path}`}
              >
                <Icon.StarFill size={12} />
                <span className="nav-clip">{f.label || baseName(f.path) || f.path}</span>
                <span className="muted" style={{ fontSize: 10.5 }}>{connName(f.connectionId)}</span>
              </button>
              <button
                className="btn icon ghost nav-row-action"
                aria-label={t('fm.removeFavorite')}
                title={t('fm.removeFavorite')}
                onClick={() => onRemoveFavorite(f.id)}
              >
                <Icon.Close size={11} />
              </button>
            </div>
          ))}
        </div>
      )}

      <button className="nav-section-head" onClick={() => setShowRecents(!showRecents)} aria-expanded={showRecents}>
        <Icon.Clock size={13} />
        <span>{t('fm.navRecent')}</span>
        <Icon.ChevronRight size={12} className={`chev ${showRecents ? 'open' : ''}`} />
      </button>
      {showRecents && (
        <div className="nav-section-body">
          {recentsFiles.length === 0 && recentsDirs.length === 0 && (
            <div className="nav-empty">{t('fm.recentsEmpty')}</div>
          )}
          {recentsFiles.map((r) => (
            <button
              key={`f:${r.connectionId}:${r.path}`}
              className="nav-section-item"
              onClick={() => onOpenRecent({ connectionId: r.connectionId, path: r.path, kind: 'file' })}
              title={`${connName(r.connectionId)}: ${r.path}`}
            >
              <Icon.Files size={13} />
              <span className="nav-clip">{baseName(r.path)}</span>
              <span className="muted" style={{ fontSize: 10.5 }}>{connName(r.connectionId)}</span>
            </button>
          ))}
          {recentsDirs.map((r) => (
            <button
              key={`d:${r.connectionId}:${r.path}`}
              className="nav-section-item"
              onClick={() => onOpenRecent({ connectionId: r.connectionId, path: r.path, kind: 'dir' })}
              title={`${connName(r.connectionId)}: ${r.path}`}
            >
              <Icon.Folder size={13} />
              <span className="nav-clip">{baseName(r.path) || r.path}</span>
              <span className="muted" style={{ fontSize: 10.5 }}>{connName(r.connectionId)}</span>
            </button>
          ))}
        </div>
      )}

      <div className="nav-section-head static">
        <Icon.Plug size={13} />
        <span>{t('fm.navServers')}</span>
      </div>
      <div className="nav-section-body">
        {connections.length === 0 && <div className="nav-empty">{t('connections.noneHint')}</div>}
        {connections.map((c) => {
          const sess = sessions.find((s) => s.connectionId === c.id && s.state !== 'disconnected');
          const isActive = activeSession?.connectionId === c.id;
          return (
            <button
              key={c.id}
              className={`nav-section-item ${isActive ? 'active' : ''}`}
              onClick={() => onConnect(c.id)}
              title={`${c.protocol}://${c.host}:${c.port}`}
            >
              <span className={`conn-dot ${sess?.state === 'connected' ? 'on' : sess?.state === 'connecting' ? 'busy' : ''}`} />
              <span className="nav-clip">{c.name}</span>
              <span className="muted" style={{ fontSize: 10.5 }}>{c.protocol}</span>
            </button>
          );
        })}
      </div>

      {hasActive && (
        <>
          <div className="nav-section-head static">
            <Icon.Server size={13} />
            <span>{t('fm.navTree')}</span>
          </div>
          <div className="nav-section-body nav-tree" role="tree">
            <div className="nav-tree-row" role="treeitem" aria-expanded={true}>
              <button className="btn icon ghost tree-toggle" aria-label={t('fm.treeExpand')} onClick={() => void loadChildren('/')}>
                <Icon.ChevronDown size={12} />
              </button>
              <button className="nav-tree-name" onClick={() => onHome()} title="/">
                <Icon.Folder size={14} />
                <span>{activeSession?.connectionName ?? '/'}</span>
              </button>
            </div>
            {rootDirs.map((d) => renderDir(d, 1))}
            {tree.loading.has('/') && rootDirs.length === 0 && (
              <div className="muted" style={{ paddingLeft: 26, fontSize: 11.5 }}>
                {t('common.loading')}
              </div>
            )}
          </div>
        </>
      )}

      {hasActive && (
        <div className="server-card" aria-label={t('fm.activeServer')}>
          <div className="server-card-head">
            <span className={`conn-dot ${activeSession.state === 'connected' ? 'on' : 'busy'}`} />
            <strong className="nav-clip">{activeSession.connectionName}</strong>
          </div>
          <div className="muted mono" style={{ fontSize: 10.5 }}>
            {activeSession.protocol}://{activeSession.host}:{connections.find((c) => c.id === activeSession.connectionId)?.port}
          </div>
          <div className="muted" style={{ fontSize: 10.5 }}>
            {connections.find((c) => c.id === activeSession.connectionId)?.username} ·{' '}
            {(activeSession.caps ?? activeSession.capabilities)?.encrypted
              ? t('fm.encrypted')
              : t('fm.plaintext')}
          </div>
          <button className="btn ghost" style={{ fontSize: 12, padding: '3px 8px', marginTop: 6 }} onClick={onDisconnect}>
            <Icon.PlugOff size={13} /> {t('connections.disconnect')}
          </button>
        </div>
      )}
    </aside>
  );
}

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  api,
  type Capabilities,
  type Entry,
  type Favorite,
  type RecentFileItem,
  type SessionInfo,
} from '../api/client';
import { useI18n } from '../i18n/i18n';
import { useStore } from '../state/store';
import { Icon } from '../components/Icons';
import { ContextMenu, type MenuItem } from '../components/ContextMenu';
import { Preview } from '../components/Preview';
import { formatBytes } from '../util/format';
import {
  buildBreadcrumbs,
  isHidden,
  normalizeRemotePath,
  planPaste,
  sortEntries,
  type PlanResult,
} from './operations';
import {
  defaultColumns,
  type ClipboardState,
  type ColumnId,
  type ColumnState,
  type ConflictPolicy,
  type HistoryEntry,
  type SearchOptions,
  type SearchState,
  type SortState,
  type ViewMode,
} from './types';
import { AddressBar } from './AddressBar';
import { NavSidebar } from './NavSidebar';
import { FileTable } from './FileTable';
import { IconView } from './IconView';
import {
  ConfirmDialog,
  ConflictDialog,
  HistoryPanel,
  InputDialog,
  MoveCopyDialog,
  PropertiesDialog,
  SearchPanel,
} from './dialogs';
import { Editor, isEditableName, type EditorTab } from './Editor';
import { LocalPane, LOCAL_DRAG_MIME } from './LocalPane';

const PREFS_KEY = 'remorasftp.fm.prefs.v1';

interface Prefs {
  viewMode?: ViewMode;
  columns?: ColumnState[];
  sort?: SortState;
  showLocal?: boolean;
}

function loadPrefs(): Prefs {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    return raw ? (JSON.parse(raw) as Prefs) : {};
  } catch {
    return {};
  }
}

interface ConnNav {
  path: string;
  back: string[];
  fwd: string[];
}

type DialogState =
  | { type: 'none' }
  | { type: 'confirm'; title: string; message: string; danger?: boolean; onConfirm: () => void }
  | { type: 'newFolder' }
  | { type: 'newFile' }
  | { type: 'rename' }
  | { type: 'movecopy'; mode: 'copy' | 'move'; items: Entry[] }
  | { type: 'conflicts'; plan: PlanResult; move: boolean }
  | { type: 'properties'; entry: Entry; isLocal?: boolean }
  | { type: 'history' };

export function FileManager({ onNavigate }: { onNavigate?: (v: string) => void }) {
  const { t } = useI18n();
  const store = useStore();
  const { sessions, connections, settings, notify, refreshSessions, transfers } = store;

  // ---- connection / navigation state ----
  const prefs = useRef(loadPrefs()).current;
  const [activeConnId, setActiveConnId] = useState<string | null>(null);
  const [navMap, setNavMap] = useState<Record<string, ConnNav>>({});
  const [entries, setEntries] = useState<Entry[]>([]);
  const [caps, setCaps] = useState<Capabilities | null>(null);
  const [loading, setLoading] = useState(false);
  const [listError, setListError] = useState<string | null>(null);
  const reqSeq = useRef(0);

  // ---- view state ----
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    const m = prefs.viewMode ?? (settings?.defaultView as ViewMode | undefined);
    return m && ['details', 'list', 'largeIcons', 'mediumIcons', 'smallIcons', 'compact'].includes(m) ? m : 'details';
  });
  const [columns, setColumns] = useState<ColumnState[]>(() => {
    const c = prefs.columns;
    if (c && Array.isArray(c) && c.length > 0) return c;
    return defaultColumns();
  });
  const [sort, setSort] = useState<SortState>(() => prefs.sort ?? { key: 'name', dir: 'asc' });
  const [showHidden, setShowHidden] = useState<boolean>(settings?.showHidden ?? false);
  const [quickFilter, setQuickFilter] = useState('');
  const [showColumns, setShowColumns] = useState(false);
  const [showLocal, setShowLocal] = useState<boolean>(prefs.showLocal ?? false);

  // ---- selection ----
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [focusedPath, setFocusedPath] = useState<string | null>(null);
  const anchorRef = useRef<number | null>(null);

  // ---- clipboard (per connection) ----
  const [clips, setClips] = useState<Record<string, ClipboardState>>({});

  // ---- dialogs ----
  const [dialog, setDialog] = useState<DialogState>({ type: 'none' });
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');

  // ---- search ----
  const [search, setSearch] = useState<SearchState>({
    active: false,
    running: false,
    options: { query: '', startsWith: false, extension: '', caseSensitive: false, includeHidden: false, recursive: true },
    results: [],
    count: 0,
    canceled: false,
  });
  const searchAbort = useRef<AbortController | null>(null);

  // ---- editor ----
  const [editorTabs, setEditorTabs] = useState<EditorTab[]>([]);
  const [editorActive, setEditorActive] = useState<string | null>(null);

  // ---- history ----
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const undoFns = useRef<Map<number, () => Promise<void>>>(new Map());

  // ---- favorites / recents ----
  const [favorites, setFavorites] = useState<Favorite[]>([]);
  const [recentsFiles, setRecentsFiles] = useState<RecentFileItem[]>([]);
  const [recentsDirs, setRecentsDirs] = useState<{ connectionId: string; path: string; visitedAt: string }[]>([]);

  // ---- context menu / misc ----
  const [menu, setMenu] = useState<{ x: number; y: number; entry?: Entry } | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const [previewEntry, setPreviewEntry] = useState<Entry | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const activeSession: SessionInfo | null = useMemo(() => {
    if (activeConnId) {
      const s = sessions.find((x) => x.connectionId === activeConnId && x.state === 'connected');
      if (s) return s;
    }
    return sessions.find((s) => s.state === 'connected') ?? null;
  }, [sessions, activeConnId]);

  // Auto-pick a connected session once.
  useEffect(() => {
    if (!activeConnId) {
      const first = sessions.find((s) => s.state === 'connected');
      if (first) setActiveConnId(first.connectionId);
    }
  }, [sessions, activeConnId]);

  const nav = activeConnId ? (navMap[activeConnId] ?? { path: '/', back: [], fwd: [] }) : { path: '/', back: [], fwd: [] };
  const path = nav.path;

  // ---- persistence ----
  useEffect(() => {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ viewMode, columns, sort, showLocal }));
  }, [viewMode, columns, sort, showLocal]);

  // ---- data loading ----
  const loadDir = useCallback(
    async (session: SessionInfo, p: string): Promise<boolean> => {
      const seq = ++reqSeq.current;
      setLoading(true);
      setListError(null);
      try {
        const r = await api.list(session.id, p);
        if (seq !== reqSeq.current) return false;
        setEntries(r.entries ?? []);
        setCaps(r.caps);
        return true;
      } catch (e) {
        if (seq !== reqSeq.current) return false;
        setListError(e instanceof Error ? e.message : 'list failed');
        return false;
      } finally {
        if (seq === reqSeq.current) setLoading(false);
      }
    },
    [],
  );

  const refresh = useCallback(async () => {
    if (!activeSession) return;
    await loadDir(activeSession, path);
    setSelection(new Set());
  }, [activeSession, path, loadDir]);

  // pendingNav: navigate to a path as soon as a connection becomes active
  // (used when a favorite/recent points at a not-yet-connected server).
  const pendingNavRef = useRef<Record<string, string>>({});

  const navigate = useCallback(
    async (target: string, opts?: { push?: boolean; session?: SessionInfo }) => {
      const sess = opts?.session ?? activeSession;
      if (!sess || sess.state !== 'connected') return;
      const p = normalizeRemotePath(target);
      const seq = ++reqSeq.current;
      setLoading(true);
      setListError(null);
      try {
        const r = await api.list(sess.id, p);
        if (seq !== reqSeq.current) return;
        setNavMap((m) => {
          const cur = m[sess.connectionId] ?? { path: '/', back: [], fwd: [] };
          const back = opts?.push === false ? cur.back : [...cur.back, cur.path];
          return { ...m, [sess.connectionId]: { path: r.path, back, fwd: [] } };
        });
        setEntries(r.entries ?? []);
        setCaps(r.caps);
        setSelection(new Set());
        setFocusedPath(null);
        setSearch((s) => ({ ...s, active: false, results: [], count: 0 }));
      } catch (e) {
        notify('error', e instanceof Error ? e.message : t('common.error'));
      } finally {
        if (seq === reqSeq.current) setLoading(false);
      }
    },
    [activeSession, notify, t],
  );

  const goBack = useCallback(() => {
    if (!activeSession || nav.back.length === 0) return;
    const target = nav.back[nav.back.length - 1];
    void navigate(target, { push: false });
  }, [activeSession, nav.back, navigate]);

  const goForward = useCallback(() => {
    if (!activeSession || nav.fwd.length === 0) return;
    const target = nav.fwd[nav.fwd.length - 1];
    void navigate(target, { push: false });
  }, [activeSession, nav.fwd, navigate]);

  const goUp = useCallback(() => {
    if (path === '/') return;
    const parent = path.slice(0, path.lastIndexOf('/')) || '/';
    void navigate(parent);
  }, [path, navigate]);

  // Load the (per-connection remembered) directory whenever the active
  // session changes, plus any pending navigation for a freshly connected
  // server (favorite/recent clicked before the connection was up).
  useEffect(() => {
    if (!activeSession) return;
    const pending = pendingNavRef.current[activeSession.connectionId];
    if (pending) {
      delete pendingNavRef.current[activeSession.connectionId];
      void navigate(pending, { session: activeSession });
    } else {
      const remembered = navMap[activeSession.connectionId]?.path ?? '/';
      void navigate(remembered, { session: activeSession, push: false });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeSession?.id]);

  // ---- derived visible list ----
  const visibleEntries = useMemo(() => {
    let list = entries.filter((e) => showHidden || !isHidden(e.name));
    if (quickFilter) {
      const q = quickFilter.toLowerCase();
      list = list.filter((e) => e.name.toLowerCase().includes(q));
    }
    return sortEntries(list, sort);
  }, [entries, showHidden, quickFilter, sort]);

  const selectedEntries = useMemo(
    () => visibleEntries.filter((e) => selection.has(e.path)),
    [visibleEntries, selection],
  );

  const crumbs = useMemo(() => buildBreadcrumbs(path, activeSession?.connectionName ?? 'Home'), [path, activeSession]);

  const clip: ClipboardState | null = activeConnId ? (clips[activeConnId] ?? null) : null;

  // ---- favorites / recents refresh ----
  const refreshFavorites = useCallback(async () => {
    try {
      const r = await api.favorites();
      setFavorites(r.favorites ?? []);
    } catch {
      /* ignore */
    }
  }, []);
  const refreshRecents = useCallback(async () => {
    try {
      const r = await api.recents();
      setRecentsFiles(r.files ?? []);
      setRecentsDirs(r.dirs ?? []);
    } catch {
      /* ignore */
    }
  }, []);
  useEffect(() => {
    void refreshFavorites();
    void refreshRecents();
  }, [refreshFavorites, refreshRecents]);

  // Refresh the list when one of our copy/move jobs finishes.
  useEffect(() => {
    const done = transfers.some(
      (j) => j.sessionId === activeSession?.id && (j.direction === 'copy' || j.direction === 'move') &&
        (j.status === 'completed' || j.status === 'failed' || j.status === 'canceled'),
    );
    if (done) void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [transfers]);

  // ---- history ----
  const addHistory = useCallback((kind: string, message: string, undoDescription?: string, undo?: () => Promise<void>) => {
    setHistory((h) => {
      const id = Date.now() + Math.floor(Math.random() * 1000);
      if (undo) undoFns.current.set(id, undo);
      return [{ id, time: new Date().toISOString(), kind, message, undoDescription }, ...h].slice(0, 50);
    });
  }, []);
  const doUndo = useCallback(async (id: number) => {
    const fn = undoFns.current.get(id);
    if (!fn) return;
    undoFns.current.delete(id);
    try {
      await fn();
      notify('success', t('fm.historyUndone'));
    } catch (e) {
      notify('error', e instanceof Error ? e.message : t('common.error'));
    }
  }, [notify, t]);

  // ---- selection ----
  const onRowClick = useCallback((e: Entry, index: number, ev: React.MouseEvent) => {
    setSelection((s) => {
      const next = new Set(s);
      if (ev.ctrlKey || ev.metaKey) {
        if (next.has(e.path)) next.delete(e.path);
        else next.add(e.path);
        anchorRef.current = index;
      } else if (ev.shiftKey && anchorRef.current !== null) {
        const [lo, hi] = [Math.min(anchorRef.current, index), Math.max(anchorRef.current, index)];
        visibleEntries.slice(lo, hi + 1).forEach((x) => next.add(x.path));
      } else {
        next.clear();
        next.add(e.path);
        anchorRef.current = index;
      }
      return next;
    });
    setFocusedPath(e.path);
  }, [visibleEntries]);

  const openEntry = useCallback(
    (e: Entry) => {
      if (!activeSession) return;
      if (e.type === 'dir') {
        void navigate(e.path);
        void api.addRecent({ connectionId: activeSession.connectionId, path: e.path, kind: 'dir' }).catch(() => {});
      } else if (isEditableName(e.name) && !e.isSymlink) {
        setEditorTabs((tabs) =>
          tabs.some((tb) => tb.entry.path === e.path)
            ? tabs
            : [
                ...tabs,
                {
                  entry: { name: e.name, path: e.path, modTime: e.modTime },
                  content: '',
                  loadedModTime: e.modTime ?? null,
                  dirty: false,
                  loaded: false,
                  preview: false,
                  findOpen: false,
                },
              ],
        );
        setEditorActive(e.path);
        void api.addRecent({ connectionId: activeSession.connectionId, path: e.path, kind: 'file' }).catch(() => {});
      } else {
        setPreviewEntry(e);
        void api.addRecent({ connectionId: activeSession.connectionId, path: e.path, kind: 'file' }).catch(() => {});
      }
    },
    [activeSession, navigate],
  );

  // ---- clipboard ops ----
  const setClipboard = useCallback(
    (items: Entry[], cut: boolean) => {
      if (!activeConnId || items.length === 0) return;
      setClips((c) => ({ ...c, [activeConnId]: { items, cut } }));
    },
    [activeConnId],
  );

  const clearClipboard = useCallback(() => {
    if (!activeConnId) return;
    setClips((c) => {
      const n = { ...c };
      delete n[activeConnId];
      return n;
    });
  }, [activeConnId]);

  const executePlan = useCallback(
    async (plan: PlanResult, move: boolean, conflictResolutions: (ConflictPolicy)[]) => {
      if (!activeSession) return;
      // conflictResolutions is indexed by position in plan.conflicts (only
      // the conflicting items); non-conflicting items have nothing to
      // resolve, so 'replace' is a no-op for them.
      const policyFor = (i: number): ConflictPolicy => {
        const from = plan.items[i].from;
        const ci = plan.conflicts.findIndex((c) => c.from === from);
        return ci >= 0 ? (conflictResolutions[ci] ?? 'replace') : 'replace';
      };
      let failed = 0;
      for (let i = 0; i < plan.items.length; i++) {
        const { from, to } = plan.items[i];
        const resolution = policyFor(i);
        const policy = resolution === 'replace' ? 'replace' : resolution === 'skip' ? 'skip' : resolution === 'rename' ? 'rename' : 'refuse';
        if (resolution === 'skip') continue;
        try {
          await api.copy(activeSession.id, from, to, move, policy);
        } catch (e) {
          failed++;
          notify('error', `${from}: ${e instanceof Error ? e.message : 'failed'}`);
        }
      }
      if (move) clearClipboard();
      const n = plan.items.length;
      addHistory(
        move ? 'move' : 'copy',
        n === 1 ? (move ? t('fm.historyMoved', { name: plan.items[0].from.split('/').pop() ?? '' }) : t('fm.historyCopied', { name: plan.items[0].from.split('/').pop() ?? '' }))
          : move ? t('fm.historyMovedMany', { count: n }) : t('fm.historyCopiedMany', { count: n }),
      );
      if (failed === 0) {
        notify('success', t('fm.operationQueued'));
        if (onNavigate) onNavigate('transfers');
      }
      void refresh();
    },
    [activeSession, clearClipboard, addHistory, notify, t, refresh, onNavigate],
  );

  const doPaste = useCallback(
    (destPath?: string, destEntries?: Entry[]) => {
      if (!activeSession || !clip) return;
      const target = destPath ?? path;
      const plan = planPaste(clip, target, destEntries ?? entries);
      if (plan.error === 'self') {
        notify('error', t('fm.pasteSelf'));
        return;
      }
      if (plan.error === 'inside-source') {
        notify('error', t('fm.pasteInsideSource'));
        return;
      }
      if (plan.items.length === 0) {
        notify('info', t('fm.pasteNoop'));
        return;
      }
      if (plan.conflicts.length > 0) {
        setDialog({ type: 'conflicts', plan, move: clip.cut });
      } else {
        void executePlan(plan, clip.cut, plan.items.map(() => 'replace'));
      }
    },
    [activeSession, clip, path, entries, notify, t, executePlan],
  );

  const removeItems = useCallback(
    (items: Entry[]) => {
      if (!activeSession || items.length === 0) return;
      const msg =
        items.length === 1
          ? t('files.deleteConfirm', { name: items[0].name })
          : t('files.deleteConfirmMany', { count: items.length });
      const run = async () => {
        let failed = 0;
        for (const item of items) {
          try {
            await api.remove(activeSession.id, item.path, item.type === 'dir');
          } catch (e) {
            failed++;
            notify('error', `${item.name}: ${e instanceof Error ? e.message : 'failed'}`);
          }
        }
        if (failed === 0) {
          addHistory('delete', items.length === 1 ? t('fm.historyDeleted', { name: items[0].name }) : t('fm.historyDeletedMany', { count: items.length }));
          notify('success', t('fm.deleted'));
        }
        setSelection(new Set());
        void refresh();
      };
      if (settings?.confirmDeletes) {
        setDialog({ type: 'confirm', title: t('files.delete'), message: msg, danger: true, onConfirm: () => { setDialog({ type: 'none' }); void run(); } });
      } else {
        void run();
      }
    },
    [activeSession, addHistory, notify, t, refresh, settings],
  );

  const commitRename = useCallback(async () => {
    if (!activeSession || !renamingPath) {
      setRenamingPath(null);
      return;
    }
    const name = renameValue.trim();
    const oldEntry = entries.find((e) => e.path === renamingPath);
    setRenamingPath(null);
    if (!name || !oldEntry || name === oldEntry.name) return;
    const parent = renamingPath.slice(0, renamingPath.lastIndexOf('/')) || '/';
    const to = parent === '/' ? `/${name}` : `${parent}/${name}`;
    try {
      await api.rename(activeSession.id, renamingPath, to);
      const oldPath = renamingPath;
      addHistory(
        'rename',
        t('fm.historyRenamed', { a: oldEntry.name, b: name }),
        t('fm.historyUndoRename'),
        async () => {
          await api.rename(activeSession.id, to, oldPath);
          void refresh();
        },
      );
      notify('success', t('fm.renamed'));
      void refresh();
    } catch (e) {
      notify('error', e instanceof Error ? e.message : t('common.error'));
    }
  }, [activeSession, renamingPath, renameValue, entries, addHistory, notify, t, refresh]);

  const createFolder = useCallback(
    async (name: string) => {
      if (!activeSession) return;
      const p = path === '/' ? `/${name}` : `${path}/${name}`;
      try {
        await api.mkdir(activeSession.id, p);
        addHistory('new', t('fm.historyNewFolder', { name }), t('fm.historyUndoNewFolder'), async () => {
          await api.remove(activeSession.id, p, true);
          void refresh();
        });
        notify('success', t('fm.folderCreated'));
        void refresh();
      } catch (e) {
        notify('error', e instanceof Error ? e.message : t('common.error'));
      }
    },
    [activeSession, path, addHistory, notify, t, refresh],
  );

  const createFile = useCallback(
    async (name: string) => {
      if (!activeSession) return;
      const dir = path;
      const body = new Blob([''], { type: 'text/plain;charset=utf-8' });
      try {
        await api.uploadStream(activeSession.id, dir, name, body, 0);
        const p = dir === '/' ? `/${name}` : `${dir}/${name}`;
        addHistory('new', t('fm.historyNewFile', { name }), t('fm.historyUndoNewFile'), async () => {
          await api.remove(activeSession.id, p, false);
          void refresh();
        });
        notify('success', t('fm.fileCreated'));
        void refresh();
        setEditorTabs((tabs) => [
          ...tabs,
          {
            entry: { name, path: p, modTime: undefined },
            content: '',
            loadedModTime: null,
            dirty: false,
            loaded: false,
            preview: false,
            findOpen: false,
          },
        ]);
        setEditorActive(p);
      } catch (e) {
        notify('error', e instanceof Error ? e.message : t('common.error'));
      }
    },
    [activeSession, path, addHistory, notify, t, refresh],
  );

  // ---- download / upload ----
  const download = useCallback(
    async (e: Entry) => {
      if (!activeSession) return;
      try {
        const token = sessionStorage.getItem('remorasftp.token');
        const res = await fetch(api.downloadUrl(activeSession.id, e.path), {
          headers: token ? { Authorization: `Bearer ${token}` } : {},
        });
        if (!res.ok) throw new Error(`${res.status}`);
        const blob = await res.blob();
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = e.name;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        notify('success', t('files.downloadDone'));
      } catch (err) {
        notify('error', err instanceof Error ? err.message : t('common.error'));
      }
    },
    [activeSession, notify, t],
  );

  const uploadFiles = useCallback(
    async (files: FileList | File[]) => {
      if (!activeSession) return;
      const token = sessionStorage.getItem('remorasftp.token');
      for (const f of Array.from(files)) {
        try {
          await fetch(api.uploadUrl(activeSession.id, path, f.name), {
            method: 'POST',
            headers: {
              Authorization: `Bearer ${token ?? ''}`,
              'X-Requested-With': 'RemoraSFTP',
              'Content-Length': String(f.size),
            },
            body: f,
          });
        } catch (e) {
          notify('error', `${f.name}: ${e instanceof Error ? e.message : 'upload failed'}`);
        }
      }
      notify('success', t('files.uploadDone'));
      void refresh();
      if (onNavigate) onNavigate('transfers');
    },
    [activeSession, path, notify, t, refresh, onNavigate],
  );

  const uploadLocalPaths = useCallback(
    async (localPaths: string[]) => {
      if (!activeSession) return;
      const token = sessionStorage.getItem('remorasftp.token');
      for (const p of localPaths) {
        const name = p.split('/').pop() ?? p;
        try {
          const res = await fetch(api.localDownloadUrl(p), { headers: { Authorization: `Bearer ${token ?? ''}` } });
          if (!res.ok) throw new Error(`${res.status}`);
          const blob = await res.blob();
          await api.uploadStream(activeSession.id, path, name, blob, blob.size);
        } catch (e) {
          notify('error', `${name}: ${e instanceof Error ? e.message : 'upload failed'}`);
        }
      }
      void refresh();
      if (onNavigate) onNavigate('transfers');
    },
    [activeSession, path, notify, refresh, onNavigate],
  );

  // ---- search ----
  const runSearch = useCallback(async () => {
    if (!activeSession || (search.options.query === '' && search.options.extension === '')) return;
    searchAbort.current?.abort();
    const controller = new AbortController();
    searchAbort.current = controller;
    setSearch((s) => ({ ...s, active: true, running: true, results: [], count: 0, canceled: false }));
    try {
      const r = await api.search(
        activeSession.id,
        {
          path,
          query: search.options.query || undefined,
          startsWith: search.options.startsWith,
          extension: search.options.extension || undefined,
          caseSensitive: search.options.caseSensitive,
          includeHidden: search.options.includeHidden,
          recursive: search.options.recursive,
        },
        controller.signal,
      );
      setSearch((s) => ({ ...s, running: false, results: r.results ?? [], count: r.count ?? 0, canceled: r.canceled ?? false }));
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        setSearch((s) => ({ ...s, running: false, canceled: true }));
      } else {
        notify('error', e instanceof Error ? e.message : t('common.error'));
        setSearch((s) => ({ ...s, running: false }));
      }
    }
  }, [activeSession, search.options, path, notify, t]);

  const cancelSearch = useCallback(() => {
    searchAbort.current?.abort();
  }, []);

  // Close the columns popup when clicking anywhere outside it.
  const columnsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!showColumns) return;
    const close = (e: MouseEvent) => {
      if (!columnsRef.current?.contains(e.target as Node)) setShowColumns(false);
    };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [showColumns]);

  // ---- connection actions (from sidebar) ----
  const connect = useCallback(
    async (connId: string) => {
      const existing = sessions.find((s) => s.connectionId === connId);
      if (existing && existing.state === 'connected') {
        setActiveConnId(connId);
        return;
      }
      try {
        await api.connect(connId);
        await refreshSessions();
        setActiveConnId(connId);
      } catch (e) {
        if (store.trustFromError && e instanceof Error) {
          store.trustFromError(e, connId);
        } else {
          notify('error', e instanceof Error ? e.message : t('common.error'));
        }
      }
    },
    [sessions, refreshSessions, notify, t, store],
  );

  const disconnect = useCallback(async () => {
    if (!activeSession) return;
    try {
      await api.disconnect(activeSession.id);
      await refreshSessions();
    } catch (e) {
      notify('error', e instanceof Error ? e.message : t('common.error'));
    }
  }, [activeSession, refreshSessions, notify, t]);

  const openFavorite = useCallback(
    (f: Favorite) => {
      void (async () => {
        setActiveConnId(f.connectionId);
        const sess = sessions.find((s) => s.connectionId === f.connectionId && s.state === 'connected');
        if (!sess) {
          pendingNavRef.current[f.connectionId] = f.path;
          await connect(f.connectionId);
          return;
        }
        if ((navMap[f.connectionId]?.path ?? '/') === f.path && sess.connectionId === activeConnId) return;
        await navigate(f.path, { session: sess });
        void refreshRecents();
      })();
    },
    [sessions, connect, navMap, navigate, refreshRecents, activeConnId],
  );

  const openRecent = useCallback(
    (r: { connectionId: string; path: string; kind: 'file' | 'dir' }) => {
      void (async () => {
        setActiveConnId(r.connectionId);
        const sess = sessions.find((s) => s.connectionId === r.connectionId && s.state === 'connected');
        if (!sess) {
          if (r.kind === 'dir') {
            pendingNavRef.current[r.connectionId] = r.path;
          } else {
            pendingNavRef.current[r.connectionId] = r.path.slice(0, r.path.lastIndexOf('/')) || '/';
          }
          await connect(r.connectionId);
          return;
        }
        if (r.kind === 'dir') {
          await navigate(r.path, { session: sess });
        } else {
          // Navigate to the parent folder, then open the file.
          const parent = r.path.slice(0, r.path.lastIndexOf('/')) || '/';
          await navigate(parent, { session: sess });
          const entry: Entry = { name: r.path.split('/').pop() ?? r.path, path: r.path, type: 'file', size: 0 };
          setEditorTabs((tabs) =>
            tabs.some((tb) => tb.entry.path === entry.path)
              ? tabs
              : [...tabs, { entry: { name: entry.name, path: entry.path }, content: '', loadedModTime: null, dirty: false, loaded: false, preview: false, findOpen: false }],
          );
          setEditorActive(entry.path);
        }
      })();
    },
    [sessions, connect, navigate],
  );

  // ---- keyboard ----
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || dialog.type !== 'none') return;
      const meta = e.ctrlKey || e.metaKey;
      if (meta && e.key.toLowerCase() === 'a') {
        e.preventDefault();
        setSelection(new Set(visibleEntries.map((x) => x.path)));
        return;
      }
      if (meta && e.key.toLowerCase() === 'c' && selectedEntries.length) {
        setClipboard(selectedEntries, false);
        return;
      }
      if (meta && e.key.toLowerCase() === 'x' && selectedEntries.length) {
        setClipboard(selectedEntries, true);
        return;
      }
      if (meta && e.key.toLowerCase() === 'v' && clip) {
        e.preventDefault();
        doPaste();
        return;
      }
      if (meta && e.key.toLowerCase() === 'd' && selectedEntries.length) {
        e.preventDefault();
        setClipboard(selectedEntries, false);
        return;
      }
      if (e.key === 'Delete' && selectedEntries.length) {
        removeItems(selectedEntries);
        return;
      }
      if (e.key === 'Backspace' && path !== '/') {
        e.preventDefault();
        goUp();
        return;
      }
      if (e.key === 'F5') {
        e.preventDefault();
        void refresh();
        return;
      }
      if (e.key === 'F2' && focusedPath) {
        e.preventDefault();
        const entry = entries.find((x) => x.path === focusedPath);
        if (entry) {
          setRenamingPath(entry.path);
          setRenameValue(entry.name);
        }
        return;
      }
      if (e.key === 'Escape') {
        if (search.active) setSearch((s) => ({ ...s, active: false }));
        else setSelection(new Set());
        setFocusedPath(null);
        return;
      }
      // Arrow navigation
      const idx = focusedPath ? visibleEntries.findIndex((x) => x.path === focusedPath) : -1;
      const step = viewMode.startsWith('large') || viewMode.startsWith('medium') || viewMode.startsWith('small') ? 8 : 10;
      if (e.key === 'ArrowDown' || e.key === 'PageDown' || e.key === 'ArrowUp' || e.key === 'PageUp' || e.key === 'Home' || e.key === 'End') {
        e.preventDefault();
        let next = idx;
        const n = visibleEntries.length;
        if (e.key === 'ArrowDown') next = Math.min(n - 1, idx + 1);
        if (e.key === 'ArrowUp') next = Math.max(0, idx <= 0 ? 0 : idx - 1);
        if (e.key === 'PageDown') next = Math.min(n - 1, idx + step);
        if (e.key === 'PageUp') next = Math.max(0, idx - step);
        if (e.key === 'Home') next = 0;
        if (e.key === 'End') next = n - 1;
        if (n > 0 && next >= 0 && visibleEntries[next]) {
          const target = visibleEntries[next];
          setFocusedPath(target.path);
          if (!e.ctrlKey && !e.metaKey && !e.shiftKey) setSelection(new Set([target.path]));
          anchorRef.current = next;
          const row = document.querySelector<HTMLElement>(
            `.file-row[data-path="${CSS.escape(target.path)}"], .grid-item[data-path="${CSS.escape(target.path)}"]`,
          );
          row?.scrollIntoView({ block: 'nearest' });
        }
      }
      if (e.key === 'Enter' && focusedPath) {
        const entry = entries.find((x) => x.path === focusedPath);
        if (entry) openEntry(entry);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [
    visibleEntries, selectedEntries, focusedPath, path, clip, dialog.type, search.active,
    viewMode, entries, doPaste, removeItems, goUp, refresh, setClipboard, openEntry,
  ]);

  // ---- context menus ----
  const entryMenu = useCallback(
    (entry: Entry): MenuItem[] => {
      const items: MenuItem[] = [
        { label: t('fm.open'), icon: <Icon.Files size={14} />, onClick: () => openEntry(entry) },
        ...(isEditableName(entry.name)
          ? [{ label: t('fm.edit'), icon: <Icon.Code size={14} />, onClick: () => openEntry(entry) }]
          : []),
        ...(entry.type !== 'dir'
          ? [{ label: t('files.download'), icon: <Icon.Download size={14} />, onClick: () => void download(entry) }]
          : []),
        { sep: true },
        { label: t('fm.cut'), icon: <Icon.Scissors size={14} />, onClick: () => setClipboard([entry], true) },
        { label: t('fm.copy'), icon: <Icon.Copy size={14} />, onClick: () => setClipboard([entry], false) },
        ...(clip ? [{ label: t('fm.paste'), icon: <Icon.Paste size={14} />, onClick: () => doPaste(entry.type === 'dir' ? entry.path : path) }] : []),
        { sep: true },
        {
          label: t('fm.moveTo'),
          icon: <Icon.ArrowLeft size={14} />,
          onClick: () => {
            const items = selectedEntries.some((s) => s.path === entry.path) ? selectedEntries : [entry];
            setDialog({ type: 'movecopy', mode: 'move', items });
          },
        },
        {
          label: t('fm.copyTo'),
          icon: <Icon.Copy size={14} />,
          onClick: () => {
            const items = selectedEntries.some((s) => s.path === entry.path) ? selectedEntries : [entry];
            setDialog({ type: 'movecopy', mode: 'copy', items });
          },
        },
        {
          label: t('files.rename'),
          icon: <Icon.Edit size={14} />,
          onClick: () => {
            setRenamingPath(entry.path);
            setRenameValue(entry.name);
          },
        },
        { sep: true },
        { label: t('inspector.properties'), icon: <Icon.Settings size={14} />, onClick: () => setDialog({ type: 'properties', entry }) },
        {
          label: t('fm.addFavorite'),
          icon: <Icon.Star size={14} />,
          onClick: () =>
            void (async () => {
              if (!activeSession) return;
              try {
                await api.addFavorite({
                  connectionId: activeSession.connectionId,
                  path: entry.path,
                  kind: entry.type === 'dir' ? 'dir' : 'file',
                  label: entry.name,
                });
                await refreshFavorites();
                notify('success', t('fm.favoriteAdded'));
              } catch (e) {
                notify('error', e instanceof Error ? e.message : t('common.error'));
              }
            })(),
        },
        { sep: true },
        { label: t('files.delete'), icon: <Icon.Trash size={14} />, danger: true, onClick: () => removeItems([entry]) },
      ];
      return items;
    },
    [t, openEntry, download, setClipboard, selectedEntries, clip, doPaste, path, activeSession, refreshFavorites, notify, removeItems],
  );

  const areaMenu = useCallback(
    (): MenuItem[] => [
      { label: t('fm.newFile'), icon: <Icon.File size={14} />, onClick: () => setDialog({ type: 'newFile' }) },
      { label: t('files.newFolder'), icon: <Icon.Folder size={14} />, onClick: () => setDialog({ type: 'newFolder' }) },
      { sep: true },
      ...(clip ? [{ label: t('fm.paste'), icon: <Icon.Paste size={14} />, onClick: () => doPaste() }] : []),
      { label: t('files.refresh'), icon: <Icon.Refresh size={14} />, onClick: () => void refresh() },
      { sep: true },
      { label: t('fm.searchTitle'), icon: <Icon.Search size={14} />, onClick: () => setSearch((s) => ({ ...s, active: true })) },
      {
        label: showHidden ? t('fm.hideHidden') : t('fm.showHidden'),
        icon: <Icon.Eye size={14} />,
        onClick: () => {
          const next = !showHidden;
          setShowHidden(next);
          void store.setSettings({ ...(settings ?? ({} as NonNullable<typeof settings>)), showHidden: next }).catch(() => {});
        },
      },
      {
        label: showLocal ? t('fm.paneClose') : t('fm.paneLocal'),
        icon: <Icon.Split size={14} />,
        onClick: () => setShowLocal(!showLocal),
      },
      { sep: true },
      { label: t('fm.historyTitle'), icon: <Icon.Clock size={14} />, onClick: () => setDialog({ type: 'history' }) },
    ],
    [t, clip, doPaste, refresh, showHidden, showLocal, store, settings],
  );

  const crumbsEl = crumbs;

  // ---- render ----
  if (!activeSession) {
    return (
      <div className="fm" role="region" aria-label={t('nav.files')}>
        <div className="empty-state" style={{ flex: 1 }}>
          <Icon.Plug className="big-icon" />
          <h2 style={{ margin: '0 0 6px' }}>{t('connections.selectToConnect')}</h2>
          <div style={{ display: 'grid', gap: 8, marginTop: 14, justifyItems: 'start' }}>
            {connections.map((c) => (
              <button key={c.id} className="btn" onClick={() => void connect(c.id)}>
                <Icon.Plug size={15} />
                {c.name}
                <span className="muted mono">
                  {c.protocol}://{c.host}:{c.port}
                </span>
              </button>
            ))}
            {connections.length === 0 && <p className="muted">{t('connections.noneHint')}</p>}
          </div>
        </div>
      </div>
    );
  }

  const searchMode = search.active && (search.results.length > 0 || search.running);

  return (
    <div className="fm" role="region" aria-label={t('nav.files')}>
      <NavSidebar
        connections={connections}
        sessions={sessions}
        activeSession={activeSession}
        currentPath={path}
        favorites={favorites}
        recentsFiles={recentsFiles}
        recentsDirs={recentsDirs}
        onHome={() => void navigate('/')}
        onNavigate={(p) => void navigate(p)}
        onOpenFavorite={openFavorite}
        onOpenRecent={openRecent}
        onRemoveFavorite={(id) =>
          void (async () => {
            try {
              await api.removeFavorite(id);
              await refreshFavorites();
            } catch {
              /* ignore */
            }
          })()
        }
        onConnect={(id) => void connect(id)}
        onDisconnect={() => void disconnect()}
      />

      <div className="fm-main" style={{ position: 'relative', display: 'flex', flexDirection: 'column', minHeight: 0 }}>
        {/* Toolbar */}
        <div className="toolbar fm-toolbar">
          <button
            className="btn icon"
            title={t('fm.newFile')}
            aria-label={t('fm.newFile')}
            onClick={() => setDialog({ type: 'newFile' })}
          >
            <Icon.File size={15} />
          </button>
          <button
            className="btn icon"
            title={t('files.newFolder')}
            aria-label={t('files.newFolder')}
            onClick={() => setDialog({ type: 'newFolder' })}
          >
            <Icon.Folder size={15} />
          </button>
          <span className="toolbar-sep" />
          <button className="btn icon" title={t('fm.cut')} aria-label={t('fm.cut')} disabled={selectedEntries.length === 0} onClick={() => setClipboard(selectedEntries, true)}>
            <Icon.Scissors size={15} />
          </button>
          <button className="btn icon" title={t('fm.copy')} aria-label={t('fm.copy')} disabled={selectedEntries.length === 0} onClick={() => setClipboard(selectedEntries, false)}>
            <Icon.Copy size={15} />
          </button>
          <button className="btn icon" title={t('fm.paste')} aria-label={t('fm.paste')} disabled={!clip} onClick={() => doPaste()}>
            <Icon.Paste size={15} />
          </button>
          <button className="btn icon" title={t('files.delete')} aria-label={t('files.delete')} disabled={selectedEntries.length === 0} onClick={() => removeItems(selectedEntries)}>
            <Icon.Trash size={15} />
          </button>
          <span className="toolbar-sep" />
          <button className="btn primary" onClick={() => fileInputRef.current?.click()}>
            <Icon.Upload size={14} /> {t('files.upload')}
          </button>
          <button
            className="btn"
            disabled={selectedEntries.length === 0}
            onClick={() => selectedEntries.forEach((e) => void download(e))}
          >
            <Icon.Download size={14} /> {t('files.download')}
          </button>
          <span className="toolbar-sep" />
          <input
            className="input"
            style={{ width: 150, fontSize: 12.5 }}
            placeholder={t('fm.filterPlaceholder')}
            value={quickFilter}
            onChange={(e) => setQuickFilter(e.target.value)}
            aria-label={t('fm.filterPlaceholder')}
          />
          <button
            className={`btn ${search.active ? 'primary' : ''}`}
            onClick={() => setSearch((s) => ({ ...s, active: !s.active }))}
            aria-expanded={search.active}
          >
            <Icon.Search size={14} /> {t('fm.searchTitle')}
          </button>
          <div className="grow" />
          <div className="row" style={{ position: 'relative', gap: 2 }}>
            <button className={`btn icon ${viewMode === 'details' || viewMode === 'list' || viewMode === 'compact' ? '' : 'ghost'}`} aria-label={t('fm.viewDetails')} title={t('fm.viewDetails')} onClick={() => setViewMode('details')}>
              <Icon.List size={15} />
            </button>
            <button className={`btn icon ${viewMode === 'largeIcons' || viewMode === 'mediumIcons' ? '' : 'ghost'}`} aria-label={t('fm.viewLarge')} title={t('fm.viewLarge')} onClick={() => setViewMode('largeIcons')}>
              <Icon.Grid size={15} />
            </button>
            <button className={`btn icon ${viewMode === 'smallIcons' ? '' : 'ghost'}`} aria-label={t('fm.viewSmall')} title={t('fm.viewSmall')} onClick={() => setViewMode('smallIcons')}>
              <Icon.Files size={15} />
            </button>
          </div>
          <div className="row" style={{ position: 'relative' }} ref={columnsRef}>
            <button className={`btn icon ${showColumns ? 'ghost' : ''}`} aria-label={t('fm.columns')} title={t('fm.columns')} aria-expanded={showColumns} onClick={() => setShowColumns(!showColumns)}>
              <Icon.Column size={15} />
            </button>
            {showColumns && (
              <div className="columns-popup" role="menu" aria-label={t('fm.columns')}>
                {columns.map((c) => (
                  <label key={c.id} className="radio-row" style={{ fontSize: 12.5 }}>
                    <input
                      type="checkbox"
                      checked={!c.hidden}
                      onChange={() =>
                        setColumns((cs) => cs.map((x) => (x.id === c.id ? { ...x, hidden: !x.hidden } : x)))
                      }
                    />
                    {t(
                      c.id === 'name' ? 'files.name' : c.id === 'type' ? 'files.kind' : c.id === 'size' ? 'files.size' : c.id === 'date' ? 'files.modified' : c.id === 'permissions' ? 'files.permissions' : c.id === 'owner' ? 'files.owner' : 'files.group',
                    )}
                  </label>
                ))}
              </div>
            )}
          </div>
          <button
            className={`btn icon ${showHidden ? 'ghost' : ''}`}
            aria-label={showHidden ? t('fm.hideHidden') : t('fm.showHidden')}
            title={showHidden ? t('fm.hideHidden') : t('fm.showHidden')}
            onClick={() => {
              const next = !showHidden;
              setShowHidden(next);
              void store.setSettings({ ...(settings ?? ({} as NonNullable<typeof settings>)), showHidden: next }).catch(() => {});
            }}
          >
            <Icon.Eye size={15} />
          </button>
          <button className="btn icon" aria-label={t('fm.paneLocal')} title={t('fm.paneLocal')} aria-pressed={showLocal} onClick={() => setShowLocal(!showLocal)}>
            <Icon.Split size={15} />
          </button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            style={{ display: 'none' }}
            onChange={(e) => {
              if (e.target.files) void uploadFiles(e.target.files);
              e.target.value = '';
            }}
          />
        </div>

        {/* Address bar */}
        <AddressBar
          crumbs={crumbsEl}
          currentPath={path}
          canBack={nav.back.length > 0}
          canForward={nav.fwd.length > 0}
          onBack={goBack}
          onForward={goForward}
          onUp={goUp}
          onNavigate={(p) => void navigate(p)}
          onRefresh={() => void refresh()}
          notify={notify}
        />

        {search.active && (
          <SearchPanel
            open
            options={search.options}
            setOptions={(o) => setSearch((s) => ({ ...s, options: o }))}
            running={search.running}
            resultCount={search.count}
            canceled={search.canceled}
            onRun={() => void runSearch()}
            onCancel={cancelSearch}
            onClear={() => setSearch((s) => ({ ...s, active: false, results: [], count: 0 }))}
          />
        )}

        {(caps ?? null) && !caps?.encrypted && (
          <div style={{ background: 'var(--warning-subtle)', color: 'var(--warning)', padding: '6px 14px', fontSize: 12.5, fontWeight: 500 }} role="alert">
            ⚠ {t('files.capabilities.plainFtp')}
          </div>
        )}

        {/* Content: editor or file area (+ local pane) */}
        <div className="fm-content" style={{ display: 'flex', flex: 1, minHeight: 0 }}>
          <div
            className="fm-filearea"
            style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', minHeight: 0, position: 'relative' }}
            onDragOver={(e) => {
              if (e.dataTransfer.types.includes('Files') || e.dataTransfer.types.includes(LOCAL_DRAG_MIME)) {
                e.preventDefault();
                setDragOver(true);
              }
            }}
            onDragLeave={(e) => {
              if (e.currentTarget === e.target) setDragOver(false);
            }}
            onDrop={(e) => {
              e.preventDefault();
              setDragOver(false);
              const local = e.dataTransfer.getData(LOCAL_DRAG_MIME);
              if (local) {
                try {
                  const paths = JSON.parse(local) as string[];
                  if (Array.isArray(paths) && paths.length > 0) void uploadLocalPaths(paths);
                } catch {
                  /* ignore malformed payload */
                }
                return;
              }
              if (e.dataTransfer.files?.length) void uploadFiles(e.dataTransfer.files);
            }}
          >
            {editorTabs.length > 0 ? (
              <Editor
                tabs={editorTabs}
                activeId={editorActive}
                session={activeSession}
                onSetActive={setEditorActive}
                onCloseTab={(id) => {
                  setEditorTabs((tabs) => tabs.filter((tb) => tb.entry.path !== id));
                  setEditorActive((cur) => (cur === id ? (editorTabs.find((tb) => tb.entry.path !== id)?.entry.path ?? null) : cur));
                }}
                onTabUpdate={(id, patch) =>
                  setEditorTabs((tabs) => tabs.map((tb) => (tb.entry.path === id ? { ...tb, ...patch } : tb)))
                }
                onSaved={(id) => {
                  const tab = editorTabs.find((tb) => tb.entry.path === id);
                  if (tab) addHistory('edit', t('fm.historySaved', { name: tab.entry.name }));
                }}
                notify={notify}
              />
            ) : (
              <>
                {searchMode ? (
                  <div style={{ padding: '6px 14px', display: 'flex', gap: 8, alignItems: 'center', borderBottom: '1px solid var(--border)' }}>
                    <strong style={{ fontSize: 13 }}>{t('fm.searchResults', { count: search.count })}</strong>
                    {search.canceled && <span className="muted" style={{ fontSize: 12 }}>{t('fm.searchCanceled')}</span>}
                    <div className="grow" />
                    <button className="btn" style={{ fontSize: 12, padding: '2px 10px' }} onClick={() => setSearch((s) => ({ ...s, active: false, results: [], count: 0 }))}>
                      {t('fm.searchBackToFolder')}
                    </button>
                  </div>
                ) : null}

                {loading && entries.length === 0 && <p className="muted" style={{ padding: 14 }}>{t('common.loading')}</p>}
                {listError && (
                  <div style={{ padding: '10px 14px', color: 'var(--danger)', fontSize: 13 }} role="alert">
                    {t('fm.loadError')}: {listError}
                  </div>
                )}
                {!loading && !listError && visibleEntries.length === 0 && (
                  <div className="empty-state" style={{ flex: 1 }}>
                    <Icon.Folder className="big-icon" />
                    <p>{searchMode ? t('common.noResults') : quickFilter ? t('common.noResults') : t('files.empty')}</p>
                    {!searchMode && !quickFilter && <p className="faint">{t('files.emptyHint')}</p>}
                  </div>
                )}

                {!loading && (searchMode ? search.results : visibleEntries).length > 0 && (
                  <>
                    {viewMode === 'details' || viewMode === 'list' || viewMode === 'compact' ? (
                      <FileTable
                        entries={searchMode ? search.results : visibleEntries}
                        columns={columns}
                        sort={sort}
                        view={viewMode === 'details' ? 'details' : viewMode === 'list' ? 'list' : 'compact'}
                        selection={selection}
                        activePath={focusedPath}
                        caps={caps ?? undefined}
                        renamingPath={renamingPath}
                        renameValue={renameValue}
                        onSort={(key: ColumnId) =>
                          setSort((s) => (s.key === key ? { key, dir: s.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }))
                        }
                        onResizeColumn={(id, width) =>
                          setColumns((cs) => cs.map((c) => (c.id === id ? { ...c, width } : c)))
                        }
                        onRowClick={onRowClick}
                        onOpen={openEntry}
                        onRowContext={(e, i, x, y) => {
                          if (!selection.has(e.path)) {
                            setSelection(new Set([e.path]));
                            setFocusedPath(e.path);
                            anchorRef.current = i;
                          }
                          setMenu({ x, y, entry: e });
                        }}
                        onAreaContext={(x, y) => setMenu({ x, y })}
                        onAreaClick={() => {
                          setSelection(new Set());
                          setFocusedPath(null);
                        }}
                        onRenameValue={setRenameValue}
                        onRenameCommit={() => void commitRename()}
                        onRenameCancel={() => setRenamingPath(null)}
                        focusedPath={focusedPath}
                      />
                    ) : (
                      <IconView
                        entries={searchMode ? search.results : visibleEntries}
                        view={viewMode}
                        selection={selection}
                        activePath={focusedPath}
                        focusedPath={focusedPath}
                        renamingPath={renamingPath}
                        renameValue={renameValue}
                        onRowClick={onRowClick}
                        onOpen={openEntry}
                        onRowContext={(e, i, x, y) => {
                          if (!selection.has(e.path)) {
                            setSelection(new Set([e.path]));
                            setFocusedPath(e.path);
                            anchorRef.current = i;
                          }
                          setMenu({ x, y, entry: e });
                        }}
                        onAreaContext={(x, y) => setMenu({ x, y })}
                        onAreaClick={() => {
                          setSelection(new Set());
                          setFocusedPath(null);
                        }}
                        onRenameValue={setRenameValue}
                        onRenameCommit={() => void commitRename()}
                        onRenameCancel={() => setRenamingPath(null)}
                      />
                    )}
                  </>
                )}

                {/* drop overlay */}
                {dragOver && <div className="drop-overlay">{t('files.dropToUpload', { path })}</div>}
              </>
            )}
          </div>

          {/* Local pane (dual view) */}
          {showLocal && (
            <LocalPane
              remotePath={path}
              onUpload={(paths) => void uploadLocalPaths(paths)}
              notify={notify}
            />
          )}
        </div>

        {/* Status bar */}
        <div className="fm-statusbar" aria-live="polite">
          <span className="muted" style={{ fontSize: 12 }}>
            {t('files.totalItems', { count: (searchMode ? search.results : visibleEntries).length })}
          </span>
          {selection.size > 0 && (
            <span className="muted" style={{ fontSize: 12 }}>
              · {t('fm.selected', { count: selection.size, size: formatBytes(selectedEntries.reduce((a, b) => a + b.size, 0)) })}
            </span>
          )}
          {clip && (
            <span className="muted" style={{ fontSize: 12 }}>
              · {clip.cut ? t('fm.clipboardCut', { count: clip.items.length }) : t('fm.clipboardCopied', { count: clip.items.length })}
            </span>
          )}
          <div className="grow" />
          <span className="muted mono" style={{ fontSize: 11.5, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '40%' }} title={path}>
            {path}
          </span>
          <span className={`conn-dot ${activeSession.state === 'connected' ? 'on' : 'busy'}`} />
          <span className="muted" style={{ fontSize: 12 }}>
            {activeSession.connectionName} · {activeSession.protocol} ·{' '}
            {(caps?.encrypted ?? true) ? t('fm.encrypted') : t('fm.plaintext')}
          </span>
        </div>
        </div>

        {/* context menu */}
      {menu && (
        <ContextMenu x={menu.x} y={menu.y} items={menu.entry ? entryMenu(menu.entry) : areaMenu()} onClose={() => setMenu(null)} />
      )}

      {/* dialogs */}
      {dialog.type === 'confirm' && (
        <ConfirmDialog
          title={dialog.title}
          message={dialog.message}
          danger={dialog.danger}
          onConfirm={dialog.onConfirm}
          onClose={() => setDialog({ type: 'none' })}
        />
      )}
      {dialog.type === 'newFolder' && (
        <InputDialog
          title={t('files.newFolder')}
          label={t('files.folderName')}
          initial={t('files.newFolderName')}
          onOk={(v) => {
            setDialog({ type: 'none' });
            void createFolder(v);
          }}
          onClose={() => setDialog({ type: 'none' })}
        />
      )}
      {dialog.type === 'newFile' && (
        <InputDialog
          title={t('fm.newFile')}
          label={t('fm.newFileName')}
          initial="untitled.txt"
          onOk={(v) => {
            setDialog({ type: 'none' });
            void createFile(v);
          }}
          onClose={() => setDialog({ type: 'none' })}
        />
      )}
      {dialog.type === 'movecopy' && (
        <MoveCopyDialog
          mode={dialog.mode}
          count={dialog.items.length}
          onClose={() => setDialog({ type: 'none' })}
          onGo={(destDir) => {
            const items = dialog.items;
            const mode = dialog.mode;
            setDialog({ type: 'none' });
            void (async () => {
              let destEntries: Entry[] = [];
              try {
                const r = await api.list(activeSession.id, destDir);
                destEntries = r.entries ?? [];
              } catch (e) {
                notify('error', e instanceof Error ? e.message : t('common.error'));
                return;
              }
              const plan = planPaste({ items, cut: mode === 'move' }, destDir, destEntries);
              if (plan.error === 'self' || plan.error === 'inside-source') {
                notify('error', t('fm.pasteInsideSource'));
                return;
              }
              if (plan.conflicts.length > 0) {
                setDialog({ type: 'conflicts', plan, move: mode === 'move' });
              } else {
                void executePlan(plan, mode === 'move', plan.items.map(() => 'replace'));
              }
            })();
          }}
        />
      )}
      {dialog.type === 'conflicts' && (
        <ConflictDialog
          conflicts={dialog.plan.conflicts}
          onCancel={() => setDialog({ type: 'none' })}
          onContinue={(resolutions) => {
            const plan = dialog.plan;
            const move = dialog.move;
            setDialog({ type: 'none' });
            void executePlan(plan, move, resolutions);
          }}
        />
      )}
      {dialog.type === 'properties' && (
        <PropertiesDialog
          entry={dialog.entry}
          session={activeSession}
          isLocal={dialog.isLocal}
          onChmod={async (octal) => {
            await api.chmod(activeSession.id, dialog.entry.path, octal);
            addHistory('chmod', t('fm.historyChmod', { name: dialog.entry.name, mode: octal }));
            notify('success', t('fm.chmodApplied'));
            void refresh();
          }}
          onAddFavorite={() =>
            void (async () => {
              try {
                await api.addFavorite({
                  connectionId: activeSession.connectionId,
                  path: dialog.entry.path,
                  kind: dialog.entry.type === 'dir' ? 'dir' : 'file',
                  label: dialog.entry.name,
                });
                await refreshFavorites();
                notify('success', t('fm.favoriteAdded'));
              } catch (e) {
                notify('error', e instanceof Error ? e.message : t('common.error'));
              }
            })()
          }
          onClose={() => setDialog({ type: 'none' })}
        />
      )}
      {dialog.type === 'history' && (
        <HistoryPanel
          entries={history}
          onUndo={(id) => void doUndo(id)}
          onClear={() => {
            setHistory([]);
            undoFns.current.clear();
          }}
          onClose={() => setDialog({ type: 'none' })}
        />
      )}

      {previewEntry && (
        <Preview
          entry={previewEntry}
          session={activeSession}
          onClose={() => setPreviewEntry(null)}
          onDownload={(e) => void download(e)}
        />
      )}
    </div>
  );
}

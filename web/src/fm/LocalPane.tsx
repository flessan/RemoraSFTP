import { useCallback, useEffect, useRef, useState } from 'react';
import { api, type Entry } from '../api/client';
import { useI18n } from '../i18n/i18n';
import { Icon, fileIconFor } from '../components/Icons';
import { formatBytes, formatDate, parentPath } from '../util/format';
import { isHidden } from './operations';

export const LOCAL_DRAG_MIME = 'application/remorasftp-local';

/**
 * The local-filesystem pane for the optional dual-pane layout. Browsing and
 * edits use the same localfs service as the rest of the app (one filesystem
 * implementation); files are transferred to the remote pane by download →
 * upload through the engine, never by the browser reaching the disk directly.
 */
export function LocalPane({
  remotePath,
  onUpload,
  notify,
}: {
  remotePath: string;
  onUpload: (localPaths: string[]) => void;
  notify: (tone: 'info' | 'success' | 'error', message: string) => void;
}) {
  const { t } = useI18n();
  const [home, setHome] = useState<string>('');
  const [path, setPath] = useState<string>('');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [loading, setLoading] = useState(false);
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [menu, setMenu] = useState<{ x: number; y: number; entry?: Entry } | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  // Close the context menu on any outside click or Escape.
  useEffect(() => {
    if (!menu) return;
    const onDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenu(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenu(null);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [menu]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const r = await api.localHome();
        if (!cancelled) setHome(r.home || '');
      } catch {
        /* ignore */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const load = useCallback(async (p: string) => {
    setLoading(true);
    try {
      const r = await api.localList(p);
      const list = (r.entries ?? []).filter((e) => !isHidden(e.name));
      list.sort((a, b) => (a.type === 'dir') !== (b.type === 'dir') ? (a.type === 'dir' ? -1 : 1) : a.name.localeCompare(b.name, undefined, { numeric: true }));
      setEntries(list);
      setPath(r.path);
      setSelection(new Set());
    } catch {
      setEntries([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (home) void load(home);
  }, [home, load]);

  const selected = entries.filter((e) => selection.has(e.path));

  const doRemove = async () => {
    if (selected.length === 0) return;
    const name = selected.length === 1 ? selected[0].name : `${selected.length} items`;
    if (!window.confirm(selected.length === 1 ? t('files.deleteConfirm', { name }) : t('files.deleteConfirmMany', { count: selected.length }))) return;
    let failed = 0;
    for (const e of selected) {
      try {
        await api.localRemove(e.path, e.type === 'dir');
      } catch (err) {
        failed++;
        notify('error', `${e.name}: ${err instanceof Error ? err.message : t('common.error')}`);
      }
    }
    if (failed === 0) notify('success', t('fm.deleted'));
    void load(path);
  };

  const doRename = async (entry: Entry) => {
    const name = window.prompt(t('files.renamePrompt'), entry.name);
    if (!name || name === entry.name) return;
    try {
      await api.localRename(entry.path, parentPath(entry.path) === '/' ? `/${name}` : `${parentPath(entry.path)}/${name}`);
      void load(path);
    } catch {
      /* ignore */
    }
  };

  const doMkdir = async () => {
    const name = window.prompt(t('files.folderName'), t('files.newFolderName'));
    if (!name) return;
    try {
      await api.localMkdir(path === '/' ? `/${name}` : `${path}/${name}`);
      void load(path);
    } catch {
      /* ignore */
    }
  };

  return (
    <div className="local-pane" role="region" aria-label={t('fm.localPane')}>
      <div className="toolbar" style={{ flexWrap: 'nowrap' }}>
        <Icon.Home size={14} />
        <span className="nav-clip mono" style={{ fontSize: 11.5 }} title={path}>
          {path}
        </span>
        <div className="grow" />
        <button className="btn icon" title={t('fm.navUp')} aria-label={t('fm.navUp')} disabled={path === '/' || path === home} onClick={() => void load(home === path ? '/' : parentPath(path))}>
          <Icon.ArrowUp size={13} />
        </button>
        <button className="btn icon" title={t('files.refresh')} aria-label={t('files.refresh')} onClick={() => void load(path)}>
          <Icon.Refresh size={13} />
        </button>
        <button className="btn icon" title={t('files.newFolder')} aria-label={t('files.newFolder')} onClick={() => void doMkdir()}>
          <Icon.Folder size={13} />
        </button>
      </div>

      <div
        className="local-pane-list"
        onClick={(e) => {
          if (e.target === e.currentTarget) setSelection(new Set());
        }}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ x: e.clientX, y: e.clientY });
        }}
      >
        {loading && entries.length === 0 && <p className="muted" style={{ padding: 10 }}>{t('common.loading')}</p>}
        {entries.map((e) => {
          const IconCmp = fileIconFor(e.name, e.type, e.isSymlink);
          return (
            <div
              key={e.path}
              className={`local-row ${selection.has(e.path) ? 'selected' : ''}`}
              draggable
              onDragStart={(ev) => {
                ev.dataTransfer.setData(LOCAL_DRAG_MIME, JSON.stringify([e.path]));
                ev.dataTransfer.effectAllowed = 'copy';
              }}
              onClick={(ev) => {
                setSelection((s) => {
                  const next = new Set(s);
                  if (ev.ctrlKey || ev.metaKey) {
                    if (next.has(e.path)) next.delete(e.path);
                    else next.add(e.path);
                  } else {
                    next.clear();
                    next.add(e.path);
                  }
                  return next;
                });
              }}
              onDoubleClick={() => {
                if (e.type === 'dir') void load(e.path);
                else onUpload([e.path]);
              }}
              onContextMenu={(ev) => {
                ev.preventDefault();
                ev.stopPropagation();
                if (!selection.has(e.path)) setSelection(new Set([e.path]));
                setMenu({ x: ev.clientX, y: ev.clientY, entry: e });
              }}
              title={e.path}
            >
              <IconCmp size={15} className={`file-icon ${e.type === 'dir' ? 'folder' : ''}`} />
              <span className="nav-clip">{e.name}</span>
              <span className="muted mono" style={{ fontSize: 10.5, flexShrink: 0 }}>
                {e.type === 'dir' ? '' : formatBytes(e.size)}
              </span>
              <span className="muted" style={{ fontSize: 10.5, flexShrink: 0, width: 84, textAlign: 'right' }}>
                {formatDate(e.modTime)}
              </span>
            </div>
          );
        })}
        {entries.length === 0 && !loading && <p className="muted" style={{ padding: 10 }}>{t('files.empty')}</p>}
      </div>

      {menu && (
        <div ref={menuRef} className="ctx-menu" style={{ left: menu.x, top: menu.y }} role="menu">
          {menu.entry && menu.entry.type !== 'dir' && (
            <button
              className="ctx-item"
              role="menuitem"
              onClick={() => {
                const p = menu.entry?.path;
                setMenu(null);
                if (p) onUpload([p]);
              }}
            >
              <Icon.Upload size={14} />
              {t('fm.uploadToRemote')} ({remotePath})
            </button>
          )}
          {menu.entry ? (
            <>
              {menu.entry.type === 'dir' && (
                <button
                  className="ctx-item"
                  role="menuitem"
                  onClick={() => {
                    setMenu(null);
                    void load(menu.entry!.path);
                  }}
                >
                  <Icon.Folder size={14} />
                  {t('fm.open')}
                </button>
              )}
              <button
                className="ctx-item"
                role="menuitem"
                onClick={() => {
                  setMenu(null);
                  void doRename(menu.entry!);
                }}
              >
                <Icon.Edit size={14} />
                {t('files.rename')}
              </button>
              <button
                className="ctx-item danger"
                role="menuitem"
                onClick={() => {
                  setMenu(null);
                  setSelection(new Set([menu.entry!.path]));
                  void doRemove();
                }}
              >
                <Icon.Trash size={14} />
                {t('files.delete')}
              </button>
            </>
          ) : (
            <>
              <button
                className="ctx-item"
                role="menuitem"
                onClick={() => {
                  setMenu(null);
                  void doMkdir();
                }}
              >
                <Icon.Folder size={14} />
                {t('files.newFolder')}
              </button>
              {selected.length > 0 && (
                <button
                  className="ctx-item"
                  role="menuitem"
                  onClick={() => {
                    setMenu(null);
                    const files = selected.filter((e) => e.type !== 'dir');
                    if (files.length > 0) onUpload(files.map((e) => e.path));
                  }}
                >
                  <Icon.Upload size={14} />
                  {t('fm.uploadToRemote')} ({selected.length})
                </button>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

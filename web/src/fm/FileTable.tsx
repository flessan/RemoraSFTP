import { useCallback, useRef } from 'react';
import type { Entry, Capabilities } from '../api/client';
import { formatBytes, formatDate } from '../util/format';
import { useI18n } from '../i18n/i18n';
import { fileIconFor } from '../components/Icons';
import { typeLabel } from './operations';
import type { ColumnId, ColumnState, SortState } from './types';

export interface FileTableProps {
  entries: Entry[];
  columns: ColumnState[];
  sort: SortState;
  view: 'details' | 'list' | 'compact';
  selection: Set<string>;
  activePath: string | null;
  caps?: Capabilities;
  renamingPath: string | null;
  renameValue: string;
  onSort: (key: ColumnId) => void;
  onResizeColumn: (id: ColumnId, width: number) => void;
  onRowClick: (e: Entry, index: number, ev: React.MouseEvent) => void;
  onOpen: (e: Entry) => void;
  onRowContext: (e: Entry, index: number, x: number, y: number) => void;
  onAreaContext: (x: number, y: number) => void;
  onAreaClick: () => void;
  onRenameValue: (v: string) => void;
  onRenameCommit: () => void;
  onRenameCancel: () => void;
  /** Marks the row as the keyboard-active row (drawn with an outline). */
  focusedPath: string | null;
}

const COLUMN_KEYS: Record<ColumnId, string> = {
  name: 'files.name',
  type: 'files.kind',
  size: 'files.size',
  date: 'files.modified',
  permissions: 'files.permissions',
  owner: 'files.owner',
  group: 'files.group',
};

export function FileTable(props: FileTableProps) {
  const { t } = useI18n();
  const {
    entries, columns, sort, view, selection, activePath, caps,
    renamingPath, renameValue, onSort, onResizeColumn,
    onRowClick, onOpen, onRowContext, onAreaContext, onAreaClick,
    onRenameValue, onRenameCommit, onRenameCancel, focusedPath,
  } = props;

  const visible = columns.filter((c) => !c.hidden);
  const resizing = useRef<{ id: ColumnId; startX: number; startW: number } | null>(null);

  const startResize = useCallback(
    (id: ColumnId, startW: number) => (ev: React.MouseEvent) => {
      ev.preventDefault();
      ev.stopPropagation();
      resizing.current = { id, startX: ev.clientX, startW };
      const move = (e: MouseEvent) => {
        const r = resizing.current;
        if (!r) return;
        const w = Math.max(60, r.startW + (e.clientX - r.startX));
        onResizeColumn(r.id, w);
      };
      const up = () => {
        resizing.current = null;
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        document.body.style.userSelect = '';
      };
      document.body.style.userSelect = 'none';
      window.addEventListener('mousemove', move);
      window.addEventListener('mouseup', up);
    },
    [onResizeColumn],
  );

  const showDate = view === 'details';
  const showPermissions = view === 'details' && !!caps?.unixPermissions;
  const effectiveColumns = visible.filter((c) => {
    if (view === 'list') return ['name', 'size'].includes(c.id);
    if (view === 'compact') return ['name', 'size', 'date'].includes(c.id);
    if (view === 'details') {
      if (c.id === 'date') return true;
      if (c.id === 'permissions') return showPermissions;
      return true;
    }
    return true;
  });

  const renderCell = (e: Entry, col: ColumnId): React.ReactNode => {
    switch (col) {
      case 'name': {
        const IconCmp = fileIconFor(e.name, e.type, e.isSymlink);
        const isRenaming = renamingPath === e.path;
        return (
          <span className="file-name-cell">
            <IconCmp className={`file-icon ${e.type === 'dir' ? 'folder' : ''}`} size={view === 'compact' ? 15 : 17} />
            {isRenaming ? (
              <input
                className="input"
                style={{ maxWidth: 260, fontSize: 13 }}
                autoFocus
                value={renameValue}
                onChange={(ev) => onRenameValue(ev.target.value)}
                onBlur={onRenameCommit}
                onClick={(ev) => ev.stopPropagation()}
                onKeyDown={(ev) => {
                  if (ev.key === 'Enter') onRenameCommit();
                  if (ev.key === 'Escape') onRenameCancel();
                  ev.stopPropagation();
                }}
              />
            ) : (
              <span className="file-name" title={e.linkTarget ? `${e.name} → ${e.linkTarget}` : e.name}>
                {e.name}
                {e.isSymlink && <span className="link-badge" title={t('files.symlinkTo', { target: e.linkTarget ?? '' })}> ↗</span>}
              </span>
            )}
          </span>
        );
      }
      case 'type':
        return <span className="muted" style={{ fontSize: 12.5 }}>{typeLabel(e, t)}</span>;
      case 'size':
        return (
          <span className="muted mono" style={{ fontSize: 12.5 }}>
            {e.type === 'dir' ? '—' : formatBytes(e.size)}
          </span>
        );
      case 'date':
        return <span className="muted" style={{ fontSize: 12.5 }}>{formatDate(e.modTime)}</span>;
      case 'permissions':
        return <span className="mono muted" style={{ fontSize: 12 }}>{e.permissions || '—'}</span>;
      case 'owner':
        return <span className="muted" style={{ fontSize: 12.5 }}>{e.owner || '—'}</span>;
      case 'group':
        return <span className="muted" style={{ fontSize: 12.5 }}>{e.group || '—'}</span>;
    }
  };

  return (
    <div
      className="file-table-wrap"
      onClick={(ev) => {
        if (ev.target === ev.currentTarget) onAreaClick();
      }}
      onContextMenu={(ev) => {
        ev.preventDefault();
        onAreaContext(ev.clientX, ev.clientY);
      }}
    >
      <table className={`files file-table file-table-${view}`}>
        <colgroup>
          {effectiveColumns.map((c) => (
            <col key={c.id} style={{ width: c.id === 'name' ? 'auto' : c.width }} />
          ))}
        </colgroup>
        <thead>
          <tr>
            {effectiveColumns.map((c) => {
              const sorted = sort.key === c.id;
              return (
                <th
                  key={c.id}
                  aria-sort={sorted ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
                  className={`th-resizable ${sorted ? 'sorted' : ''}`}
                >
                  <span
                    className="th-label"
                    role="button"
                    tabIndex={0}
                    onClick={() => onSort(c.id)}
                    onKeyDown={(ev) => {
                      if (ev.key === 'Enter' || ev.key === ' ') onSort(c.id);
                    }}
                  >
                    {t(COLUMN_KEYS[c.id])}
                    {sorted && <span className="sort-arrow">{sort.dir === 'asc' ? ' ▲' : ' ▼'}</span>}
                  </span>
                  <span
                    className="th-resize-handle"
                    aria-hidden="true"
                    onMouseDown={startResize(c.id, c.width)}
                  />
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {entries.map((e, i) => {
            const isSel = selection.has(e.path);
            const isFocused = focusedPath === e.path;
            return (
              <tr
                key={e.path}
                data-path={e.path}
                className={`file-row ${isSel ? 'selected' : ''} ${isFocused ? 'focused' : ''} ${
                  activePath === e.path ? 'active' : ''
                } ${view === 'list' ? 'row-list' : ''}`}
                onClick={(ev) => onRowClick(e, i, ev)}
                onDoubleClick={() => onOpen(e)}
                onContextMenu={(ev) => {
                  ev.preventDefault();
                  ev.stopPropagation();
                  onRowContext(e, i, ev.clientX, ev.clientY);
                }}
              >
                {effectiveColumns.map((c) => (
                  <td key={c.id}>{renderCell(e, c.id)}</td>
                ))}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

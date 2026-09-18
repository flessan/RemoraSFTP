import type { Entry } from '../api/client';
import { formatBytes } from '../util/format';
import { useI18n } from '../i18n/i18n';
import { fileIconFor } from '../components/Icons';
import type { ViewMode } from './types';

const ICON_SIZES: Record<string, number> = {
  largeIcons: 52,
  mediumIcons: 36,
  smallIcons: 24,
};

export function IconView({
  entries,
  view,
  selection,
  activePath,
  focusedPath,
  renamingPath,
  renameValue,
  onRowClick,
  onOpen,
  onRowContext,
  onAreaContext,
  onAreaClick,
  onRenameValue,
  onRenameCommit,
  onRenameCancel,
}: {
  entries: Entry[];
  view: ViewMode;
  selection: Set<string>;
  activePath: string | null;
  focusedPath: string | null;
  renamingPath: string | null;
  renameValue: string;
  onRowClick: (e: Entry, index: number, ev: React.MouseEvent) => void;
  onOpen: (e: Entry) => void;
  onRowContext: (e: Entry, index: number, x: number, y: number) => void;
  onAreaContext: (x: number, y: number) => void;
  onAreaClick: () => void;
  onRenameValue: (v: string) => void;
  onRenameCommit: () => void;
  onRenameCancel: () => void;
}) {
  const { t } = useI18n();
  const iconSize = ICON_SIZES[view] ?? 36;

  return (
    <div
      className={`icon-view icon-view-${view}`}
      onClick={(ev) => {
        if (ev.target === ev.currentTarget) onAreaClick();
      }}
      onContextMenu={(ev) => {
        ev.preventDefault();
        onAreaContext(ev.clientX, ev.clientY);
      }}
    >
      {entries.map((e, i) => {
        const IconCmp = fileIconFor(e.name, e.type, e.isSymlink);
        const isSel = selection.has(e.path);
        const isFocused = focusedPath === e.path;
        return (
          <div
            key={e.path}
            data-path={e.path}
            className={`grid-item ${isSel ? 'selected' : ''} ${isFocused ? 'focused' : ''} ${
              activePath === e.path ? 'active' : ''
            }`}
            onClick={(ev) => onRowClick(e, i, ev)}
            onDoubleClick={() => onOpen(e)}
            onContextMenu={(ev) => {
              ev.preventDefault();
              ev.stopPropagation();
              onRowContext(e, i, ev.clientX, ev.clientY);
            }}
            title={e.name}
            role="button"
            tabIndex={-1}
            aria-selected={isSel}
          >
            <div className="grid-icon" style={{ fontSize: iconSize * 0.85 }}>
              <IconCmp
                className={`file-icon ${e.type === 'dir' ? 'folder' : ''}`}
                size={iconSize}
              />
            </div>
            <div className="grid-name">
              {renamingPath === e.path ? (
                <input
                  className="input"
                  style={{ width: '100%', fontSize: 12.5, padding: '2px 6px' }}
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
                <span className="file-name" style={{ wordBreak: 'break-word' }}>
                  {e.name}
                  {e.isSymlink && (
                    <span className="link-badge" title={t('files.symlinkTo', { target: e.linkTarget ?? '' })}>
                      {' '}↗
                    </span>
                  )}
                </span>
              )}
            </div>
            {view !== 'smallIcons' && (
              <div className="faint" style={{ fontSize: 10.5 }}>
                {e.type === 'dir' ? '' : formatBytes(e.size)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

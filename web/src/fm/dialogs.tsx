import { useEffect, useMemo, useState } from 'react';
import type { Entry, SessionInfo } from '../api/client';
import { formatBytes, formatDate } from '../util/format';
import { useI18n } from '../i18n/i18n';
import { Modal } from '../components/Modal';
import { Icon } from '../components/Icons';
import { typeLabel } from './operations';
import type { ConflictItem, ConflictPolicy, HistoryEntry, SearchOptions } from './types';

// ---------- generic confirm ----------

export function ConfirmDialog({
  title,
  message,
  danger,
  confirmLabel,
  onConfirm,
  onClose,
}: {
  title: string;
  message: string;
  danger?: boolean;
  confirmLabel?: string;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button
            className={`btn ${danger ? 'danger' : 'primary'}`}
            data-autofocus="true"
            onClick={onConfirm}
          >
            {confirmLabel ?? t('common.confirm')}
          </button>
        </>
      }
    >
      <p style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{message}</p>
    </Modal>
  );
}

// ---------- new item / rename (single input) ----------

export function InputDialog({
  title,
  label,
  initial,
  placeholder,
  okLabel,
  onOk,
  onClose,
}: {
  title: string;
  label: string;
  initial: string;
  placeholder?: string;
  okLabel?: string;
  onOk: (value: string) => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [value, setValue] = useState(initial);
  const valid = value.trim().length > 0 && !value.includes('/');
  return (
    <Modal
      title={title}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button className="btn primary" data-autofocus="true" disabled={!valid} onClick={() => onOk(value.trim())}>
            {okLabel ?? t('common.ok')}
          </button>
        </>
      }
    >
      <label className="field-label" htmlFor="input-dialog-value">
        {label}
      </label>
      <input
        id="input-dialog-value"
        className="input"
        autoFocus
        value={value}
        placeholder={placeholder}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && valid) onOk(value.trim());
        }}
      />
      {!valid && <p className="muted" style={{ fontSize: 12, marginTop: 6 }}>{t('fm.invalidName')}</p>}
    </Modal>
  );
}

// ---------- move to / copy to ----------

export function MoveCopyDialog({
  mode,
  count,
  onGo,
  onClose,
}: {
  mode: 'copy' | 'move';
  count: number;
  onGo: (destDir: string) => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [dest, setDest] = useState('/');
  const isCopy = mode === 'copy';
  return (
    <Modal
      title={isCopy ? t('fm.copyTo') : t('fm.moveTo')}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button
            className="btn primary"
            data-autofocus="true"
            onClick={() => onGo(dest.trim() || '/')}
          >
            {isCopy ? t('fm.copyToGo') : t('fm.moveToGo')}
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0, fontSize: 13 }}>
        {count === 1 ? t('fm.moveCopyOne') : t('fm.moveCopyMany', { count })}
      </p>
      <label className="field-label" htmlFor="movecopy-dest">
        {t('fm.destinationFolder')}
      </label>
      <input
        id="movecopy-dest"
        className="input mono"
        autoFocus
        value={dest}
        spellCheck={false}
        onChange={(e) => setDest(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') onGo(dest.trim() || '/');
        }}
      />
    </Modal>
  );
}

// ---------- conflicts ----------

export function ConflictDialog({
  conflicts,
  onContinue,
  onCancel,
}: {
  conflicts: ConflictItem[];
  onContinue: (resolutions: (ConflictPolicy)[]) => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const [resolutions, setResolutions] = useState<ConflictPolicy[]>(conflicts.map(() => 'rename'));
  const [applyAll, setApplyAll] = useState<ConflictPolicy | ''>('');
  const setForAll = (p: ConflictPolicy) => {
    setApplyAll(p);
    setResolutions(conflicts.map(() => p));
  };
  const setOne = (i: number, p: ConflictPolicy) => {
    setApplyAll('');
    setResolutions((r) => r.map((x, j) => (j === i ? p : x)));
  };
  return (
    <Modal
      title={t('fm.conflictTitle')}
      onClose={onCancel}
      wide
      footer={
        <>
          <button className="btn" onClick={onCancel}>
            {t('common.cancel')}
          </button>
          <button className="btn primary" data-autofocus="true" onClick={() => onContinue(resolutions)}>
            {t('fm.continue')}
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0, fontSize: 13 }}>
        {t('fm.conflictHint')}
      </p>
      {applyAll !== '' && (
        <p style={{ fontSize: 13 }}>
          {t('fm.applyAllNote', { policy: t(`fm.policy.${applyAll}`) })}
        </p>
      )}
      <div className="conflict-list" role="list" aria-label={t('fm.conflictTitle')}>
        {conflicts.map((c, i) => (
          <div key={i} className="conflict-item" role="listitem">
            <div className="conflict-name">
              <span className="mono" title={c.to}>
                {c.destName}
              </span>
              <span className="muted" style={{ fontSize: 12 }}>
                {c.sourceIsDir
                  ? t('fm.conflictFolder')
                  : t('fm.conflictSizes', { a: formatBytes(c.sourceSize), b: formatBytes(c.destSize ?? 0) })}
              </span>
            </div>
            <div className="row" role="radiogroup" aria-label={c.destName} style={{ gap: 12 }}>
              {(['replace', 'skip', 'rename'] as ConflictPolicy[]).map((p) => (
                <label key={p} className="radio-row">
                  <input
                    type="radio"
                    name={`conflict-${i}`}
                    checked={resolutions[i] === p}
                    onChange={() => setOne(i, p)}
                  />
                  {t(`fm.policy.${p}`)}
                </label>
              ))}
            </div>
          </div>
        ))}
      </div>
      <div className="row" style={{ marginTop: 10, gap: 12, fontSize: 13 }}>
        <span className="muted">{t('fm.applyToAll')}:</span>
        {(['replace', 'skip', 'rename'] as ConflictPolicy[]).map((p) => (
          <label key={p} className="radio-row">
            <input type="radio" name="conflict-all" checked={applyAll === p} onChange={() => setForAll(p)} />
            {t(`fm.policy.${p}`)}
          </label>
        ))}
      </div>
    </Modal>
  );
}

// ---------- properties (details + permissions) ----------

export interface PermBit {
  owner: [boolean, boolean, boolean];
  group: [boolean, boolean, boolean];
  other: [boolean, boolean, boolean];
}

function modeToBits(mode: number): PermBit {
  return {
    owner: [!!(mode & 0o400), !!(mode & 0o200), !!(mode & 0o100)],
    group: [!!(mode & 0o040), !!(mode & 0o020), !!(mode & 0o010)],
    other: [!!(mode & 0o004), !!(mode & 0o002), !!(mode & 0o001)],
  };
}

function bitsToMode(b: PermBit): number {
  let m = 0;
  b.owner.forEach((v, i) => {
    if (v) m |= [0o400, 0o200, 0o100][i];
  });
  b.group.forEach((v, i) => {
    if (v) m |= [0o040, 0o020, 0o010][i];
  });
  b.other.forEach((v, i) => {
    if (v) m |= [0o004, 0o002, 0o001][i];
  });
  return m;
}

export function PropertiesDialog({
  entry,
  session,
  isLocal,
  onChmod,
  onAddFavorite,
  onClose,
}: {
  entry: Entry;
  session: SessionInfo | null;
  isLocal?: boolean;
  onChmod: (octal: string) => Promise<void>;
  onAddFavorite: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const caps = session?.caps ?? session?.capabilities;
  const canChmod = !isLocal && !!caps?.chmod;
  const [bits, setBits] = useState<PermBit>(() => (entry.mode ? modeToBits(entry.mode) : { owner: [true, true, true], group: [true, false, false], other: [true, false, false] }));
  const [octal, setOctal] = useState('');
  const [octalError, setOctalError] = useState('');
  const [applying, setApplying] = useState(false);

  const rows: (keyof PermBit)[] = ['owner', 'group', 'other'];
  const colLabels = [t('fm.permRead'), t('fm.permWrite'), t('fm.permExec')];
  const rowLabels = [t('fm.permOwner'), t('fm.permGroup'), t('fm.permOthers')];

  const applyOctal = () => {
    const v = octal.trim();
    const n = Number.parseInt(v, 8);
    if (v.length === 0 || v.length > 3 || Number.isNaN(n) || n > 0o777) {
      setOctalError(t('fm.invalidOctal'));
      return;
    }
    setBits(modeToBits(n & 0o777));
    setOctalError('');
  };

  const apply = async () => {
    setApplying(true);
    try {
      await onChmod(bitsToMode(bits).toString(8));
      onClose();
    } finally {
      setApplying(false);
    }
  };

  return (
    <Modal title={t('inspector.properties')} onClose={onClose} wide>
      <div className="props-grid">
        <span className="muted">{t('inspector.fileName')}</span>
        <span>{entry.name}</span>
        <span className="muted">{t('files.kind')}</span>
        <span>{typeLabel(entry, t)}</span>
        {entry.mimeType && (
          <>
            <span className="muted">{t('inspector.mimeType')}</span>
            <span className="mono">{entry.mimeType}</span>
          </>
        )}
        {entry.type !== 'dir' && (
          <>
            <span className="muted">{t('inspector.size')}</span>
            <span>{formatBytes(entry.size)}</span>
          </>
        )}
        <span className="muted">{t('inspector.modified')}</span>
        <span>{formatDate(entry.modTime)}</span>
        <span className="muted">{t('inspector.path')}</span>
        <span className="mono" style={{ wordBreak: 'break-all' }}>{entry.path}</span>
        {entry.owner && (
          <>
            <span className="muted">{t('inspector.owner')}</span>
            <span>{entry.owner}</span>
          </>
        )}
        {entry.group && (
          <>
            <span className="muted">{t('inspector.group')}</span>
            <span>{entry.group}</span>
          </>
        )}
        {entry.isSymlink && entry.linkTarget && (
          <>
            <span className="muted">{t('inspector.target')}</span>
            <span className="mono" style={{ wordBreak: 'break-all' }}>{entry.linkTarget}</span>
          </>
        )}
      </div>

      {canChmod && (
        <div className="chmod-box" style={{ marginTop: 14 }}>
          <div className="row" style={{ justifyContent: 'space-between', marginBottom: 8 }}>
            <strong style={{ fontSize: 13 }}>{t('inspector.changePermissions')}</strong>
            <span className="row" style={{ gap: 6 }}>
              <label className="muted" style={{ fontSize: 12 }} htmlFor="chmod-octal">
                {t('fm.octal')}
              </label>
              <input
                id="chmod-octal"
                className="input mono"
                style={{ width: 64 }}
                value={octal}
                placeholder="644"
                maxLength={3}
                onChange={(e) => setOctal(e.target.value.replace(/[^0-7]/g, ''))}
                onBlur={applyOctal}
                onKeyDown={(e) => e.key === 'Enter' && applyOctal()}
              />
            </span>
          </div>
          {octalError && (
            <p style={{ color: 'var(--danger)', fontSize: 12, margin: '0 0 6px' }}>{octalError}</p>
          )}
          <table className="chmod-table" aria-label={t('inspector.changePermissions')}>
            <thead>
              <tr>
                <th />
                {colLabels.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, ri) => (
                <tr key={r}>
                  <td className="muted">{rowLabels[ri]}</td>
                  {[0, 1, 2].map((ci) => {
                    const on = bits[r][ci];
                    return (
                      <td key={ci}>
                        <input
                          type="checkbox"
                          checked={on}
                          aria-label={`${rowLabels[ri]} ${colLabels[ci]}`}
                          onChange={() =>
                            setBits((b) => {
                              const next = {
                                owner: [...b.owner] as [boolean, boolean, boolean],
                                group: [...b.group] as [boolean, boolean, boolean],
                                other: [...b.other] as [boolean, boolean, boolean],
                              };
                              next[r][ci] = !on;
                              return next;
                            })
                          }
                        />
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
          <div className="row" style={{ justifyContent: 'flex-end', marginTop: 10 }}>
            <button className="btn primary" disabled={applying} onClick={() => void apply()}>
              {applying ? t('common.loading') : t('inspector.apply')}
            </button>
          </div>
        </div>
      )}

      {!canChmod && !isLocal && (
        <p className="muted" style={{ fontSize: 12.5, marginTop: 12 }}>
          {t('files.capabilities.chmodUnsupported')}
        </p>
      )}
      {isLocal && (
        <p className="muted" style={{ fontSize: 12.5, marginTop: 12 }}>
          {t('fm.localNote')}
        </p>
      )}

      <div className="row" style={{ justifyContent: 'flex-end', gap: 8, marginTop: 14 }}>
        <button className="btn" onClick={onAddFavorite}>
          <Icon.Star size={14} /> {t('fm.addFavorite')}
        </button>
      </div>
    </Modal>
  );
}

// ---------- search panel (inline, below toolbar) ----------

export function SearchPanel({
  open,
  options,
  setOptions,
  running,
  resultCount,
  canceled,
  onRun,
  onCancel,
  onClear,
}: {
  open: boolean;
  options: SearchOptions;
  setOptions: (o: SearchOptions) => void;
  running: boolean;
  resultCount: number;
  canceled: boolean;
  onRun: () => void;
  onCancel: () => void;
  onClear: () => void;
}) {
  const { t } = useI18n();
  const empty = useMemo(
    () => !options.query && !options.extension,
    [options.query, options.extension],
  );
  if (!open) return null;
  return (
    <div className="search-panel" role="search" aria-label={t('fm.searchTitle')}>
      <div className="row wrap" style={{ gap: 10 }}>
        <input
          className="input"
          style={{ flex: 1, minWidth: 180 }}
          placeholder={t('fm.searchQuery')}
          value={options.query}
          autoFocus
          spellCheck={false}
          onChange={(e) => setOptions({ ...options, query: e.target.value })}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !empty) onRun();
            if (e.key === 'Escape') onClear();
          }}
        />
        <label className="radio-row mono" title={t('fm.searchExtHint')}>
          <input
            className="input mono"
            style={{ width: 90 }}
            placeholder=".txt"
            value={options.extension}
            onChange={(e) => setOptions({ ...options, extension: e.target.value })}
          />
        </label>
        <label className="radio-row">
          <input
            type="checkbox"
            checked={options.recursive}
            onChange={(e) => setOptions({ ...options, recursive: e.target.checked })}
          />
          {t('fm.searchRecursive')}
        </label>
        <label className="radio-row">
          <input
            type="checkbox"
            checked={options.startsWith}
            onChange={(e) => setOptions({ ...options, startsWith: e.target.checked })}
          />
          {t('fm.searchStartsWith')}
        </label>
        <label className="radio-row">
          <input
            type="checkbox"
            checked={options.caseSensitive}
            onChange={(e) => setOptions({ ...options, caseSensitive: e.target.checked })}
          />
          {t('fm.searchCase')}
        </label>
        <label className="radio-row">
          <input
            type="checkbox"
            checked={options.includeHidden}
            onChange={(e) => setOptions({ ...options, includeHidden: e.target.checked })}
          />
          {t('fm.searchHidden')}
        </label>
      </div>
      <div className="row" style={{ marginTop: 8, gap: 8 }}>
        <button className="btn primary" disabled={empty || running} onClick={onRun}>
          {running ? t('common.loading') : t('fm.searchGo')}
        </button>
        {running ? (
          <button className="btn" onClick={onCancel}>
            {t('fm.searchCancel')}
          </button>
        ) : (
          <button className="btn" onClick={onClear}>
            {t('common.close')}
          </button>
        )}
        {resultCount > 0 && (
          <span className="muted" aria-live="polite">
            {t('fm.searchResults', { count: resultCount })}
            {canceled ? ` (${t('fm.searchCanceled')})` : ''}
          </span>
        )}
      </div>
    </div>
  );
}

// ---------- activity / history panel ----------

export function HistoryPanel({
  entries,
  onUndo,
  onClear,
  onClose,
}: {
  entries: HistoryEntry[];
  onUndo: (id: number) => void;
  onClear: () => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  return (
    <Modal
      title={t('fm.historyTitle')}
      onClose={onClose}
      wide
      footer={
        <>
          <button className="btn" onClick={onClear}>
            {t('activity.clear')}
          </button>
          <button className="btn primary" data-autofocus="true" onClick={onClose}>
            {t('common.close')}
          </button>
        </>
      }
    >
      <p className="muted" style={{ marginTop: 0, fontSize: 12.5 }}>
        {t('fm.historyHint')}
      </p>
      {entries.length === 0 ? (
        <p className="muted">{t('fm.historyEmpty')}</p>
      ) : (
        <div className="history-list" style={{ maxHeight: 320, overflowY: 'auto' }}>
          {entries.map((h) => (
            <div key={h.id} className="history-row">
              <span className="muted mono" style={{ fontSize: 11.5, flexShrink: 0 }}>
                {formatDate(h.time)}
              </span>
              <span className="kind-badge" data-kind={h.kind}>{t(`fm.historyKind.${h.kind}`, {} as Record<string, string | number>)}</span>
              <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={h.message}>
                {h.message}
              </span>
              {h.undoDescription && (
                <button className="btn ghost" style={{ fontSize: 12, padding: '2px 8px' }} onClick={() => onUndo(h.id)}>
                  {t('fm.historyUndo')}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
    </Modal>
  );
}

// ---------- rename-in-place is handled inline in the file views ----------

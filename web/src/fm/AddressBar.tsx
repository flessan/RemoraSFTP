import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../i18n/i18n';
import { Icon } from '../components/Icons';
import type { Breadcrumb } from './types';
import { normalizeRemotePath as normalizePath } from './operations';

export function AddressBar({
  crumbs,
  currentPath,
  canBack,
  canForward,
  onBack,
  onForward,
  onUp,
  onNavigate,
  onRefresh,
  notify,
}: {
  crumbs: Breadcrumb[];
  currentPath: string;
  canBack: boolean;
  canForward: boolean;
  onBack: () => void;
  onForward: () => void;
  onUp: () => void;
  onNavigate: (path: string) => void;
  onRefresh: () => void;
  notify: (tone: 'info' | 'success' | 'error', message: string) => void;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(currentPath);
  const [invalid, setInvalid] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (editing) {
      inputRef.current?.focus();
      inputRef.current?.select();
    }
  }, [editing]);

  // If the list load of the requested path failed, the parent reverts the
  // path; invalid feedback is set here on Enter with a path that fails
  // validation (empty segments etc.) - real server errors are toasted by
  // the parent.
  const commit = () => {
    if (!draft.trim()) {
      // Empty address: keep the current location and flag the input.
      setDraft(currentPath);
      setInvalid(true);
      setEditing(false);
      return;
    }
    const p = normalizePath(draft);
    setEditing(false);
    setInvalid(false);
    if (p === currentPath) return;
    onNavigate(p);
  };

  const copyPath = async () => {
    try {
      await navigator.clipboard.writeText(currentPath);
      notify('success', t('common.copied'));
    } catch {
      notify('error', t('common.error'));
    }
  };

  return (
    <div className="address-bar" role="navigation" aria-label={t('fm.addressBar')}>
      <div className="row" style={{ gap: 2, flexShrink: 0 }}>
        <button className="btn icon" disabled={!canBack} onClick={onBack} aria-label={t('fm.navBack')} title={t('fm.navBack')}>
          <Icon.ArrowLeft size={15} />
        </button>
        <button className="btn icon" disabled={!canForward} onClick={onForward} aria-label={t('fm.navForward')} title={t('fm.navForward')}>
          <Icon.ArrowLeft size={15} style={{ transform: 'scaleX(-1)' }} />
        </button>
        <button className="btn icon" disabled={currentPath === '/'} onClick={onUp} aria-label={t('fm.navUp')} title={t('fm.navUp')}>
          <Icon.ArrowUp size={15} />
        </button>
        <button className="btn icon" onClick={onRefresh} aria-label={t('files.refresh')} title={t('files.refresh')}>
          <Icon.Refresh size={15} />
        </button>
      </div>

      <div
        className={`address-input-wrap ${invalid ? 'invalid' : ''}`}
        onDoubleClick={() => {
          setDraft(currentPath);
          setEditing(true);
        }}
      >
        <Icon.Plug size={13} className="address-icon" />
        {editing ? (
          <input
            ref={inputRef}
            className="input mono address-input"
            aria-label={t('fm.addressEdit')}
            value={draft}
            spellCheck={false}
            onChange={(e) => {
              setDraft(e.target.value);
              setInvalid(false);
            }}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commit();
              if (e.key === 'Escape') {
                setDraft(currentPath);
                setInvalid(false);
                setEditing(false);
              }
            }}
          />
        ) : (
          <div className="address-crumbs" aria-label="Breadcrumb">
            {crumbs.map((c, i) => (
              <span key={c.path} className="row" style={{ gap: 2, flexShrink: i === crumbs.length - 1 ? 0 : undefined }}>
                {i > 0 && <span className="crumb-sep" aria-hidden="true">/</span>}
                <button
                  className={`crumb ${i === crumbs.length - 1 ? 'current' : ''}`}
                  aria-current={i === crumbs.length - 1 ? 'page' : undefined}
                  onClick={() => (c.path !== currentPath ? onNavigate(c.path) : undefined)}
                  onDoubleClick={() => {
                    setDraft(c.path);
                    setEditing(true);
                  }}
                  title={c.path}
                >
                  {c.name}
                </button>
              </span>
            ))}
            <span className="grow" />
          </div>
        )}
        {invalid && <span className="address-invalid" role="alert">{t('fm.invalidPath')}</span>}
      </div>

      <div className="row" style={{ gap: 2, flexShrink: 0 }}>
        <button
          className="btn icon"
          onClick={() => {
            setDraft(currentPath);
            setEditing(true);
          }}
          aria-label={t('fm.addressEdit')}
          title={t('fm.addressEdit')}
        >
          <Icon.Edit size={14} />
        </button>
        <button className="btn icon" onClick={() => void copyPath()} aria-label={t('files.copyPath')} title={t('files.copyPath')}>
          <Icon.Copy size={14} />
        </button>
      </div>
    </div>
  );
}

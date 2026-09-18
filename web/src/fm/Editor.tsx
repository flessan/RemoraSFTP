import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { SessionInfo } from '../api/client';
import { api } from '../api/client';
import { useI18n } from '../i18n/i18n';
import { formatBytes, formatDate, parentPath } from '../util/format';
import { Icon } from '../components/Icons';

export interface EditorTab {
  entry: { name: string; path: string; modTime?: string };
  content: string;
  loadedModTime: string | null;
  dirty: boolean;
  loaded: boolean;
  preview: boolean; // markdown preview mode (only for .md)
  findOpen: boolean;
}

const TEXT_EXTENSIONS = new Set([
  'txt', 'log', 'md', 'markdown', 'json', 'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg',
  'csv', 'tsv', 'xml', 'html', 'htm', 'css', 'scss', 'js', 'ts', 'jsx', 'tsx', 'mjs',
  'go', 'py', 'rb', 'php', 'sh', 'bash', 'zsh', 'ps1', 'bat', 'c', 'h', 'cpp', 'hpp',
  'rs', 'java', 'kt', 'swift', 'sql', 'graphql', 'proto', 'dockerfile', 'makefile',
  'env', 'gitignore', 'editorconfig', 'properties', 'lua', 'pl', 'r', 'dart', 'vue',
  'svelte', 'lock', 'sum', 'mod', 'tf', 'hcl', 'nix', 'ex', 'exs', 'erl', 'hs', 'ml',
]);

export function isEditableName(name: string): boolean {
  const lower = name.toLowerCase();
  const ext = lower.includes('.') ? (lower.split('.').pop() ?? '') : lower;
  return TEXT_EXTENSIONS.has(ext);
}

// --- minimal, safe Markdown → HTML (no script, no arbitrary URL schemes) ---
function mdEscape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function mdInline(s: string): string {
  return s
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/\*([^*]+)\*/g, '<em>$1</em>')
    .replace(
      /\[([^\]]+)\]\(([^)\s]+)\)/g,
      (_m, label, href) =>
        /^(https?:|mailto:|#)/i.test(href)
          ? `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`
          : label,
    );
}

export function renderMarkdown(src: string): string {
  const lines = mdEscape(src).split('\n');
  const out: string[] = [];
  let inCode = false;
  let inList = false;
  for (const line of lines) {
    if (line.trim().startsWith('```')) {
      if (inList) {
        out.push('</ul>');
        inList = false;
      }
      out.push(inCode ? '</code></pre>' : '<pre><code>');
      inCode = !inCode;
      continue;
    }
    if (inCode) {
      out.push(line);
      continue;
    }
    const h = line.match(/^(#{1,6})\s+(.*)$/);
    if (h) {
      if (inList) {
        out.push('</ul>');
        inList = false;
      }
      const lvl = h[1].length;
      out.push(`<h${lvl}>${mdInline(h[2])}</h${lvl}>`);
      continue;
    }
    const li = line.match(/^\s*[-*]\s+(.*)$/);
    if (li) {
      if (!inList) {
        out.push('<ul>');
        inList = true;
      }
      out.push(`<li>${mdInline(li[1])}</li>`);
      continue;
    }
    if (inList) {
      out.push('</ul>');
      inList = false;
    }
    if (/^---+$/.test(line.trim())) {
      out.push('<hr>');
      continue;
    }
    if (line.trim() === '') {
      out.push('');
      continue;
    }
    out.push(`<p>${mdInline(line)}</p>`);
  }
  if (inList) out.push('</ul>');
  if (inCode) out.push('</code></pre>');
  return out.join('\n');
}

export function isMarkdownName(name: string): boolean {
  const n = name.toLowerCase();
  return n.endsWith('.md') || n.endsWith('.markdown');
}

export function Editor({
  tabs,
  activeId,
  session,
  onSetActive,
  onCloseTab,
  onTabUpdate,
  onSaved,
  notify,
}: {
  tabs: EditorTab[];
  activeId: string | null;
  session: SessionInfo;
  onSetActive: (id: string) => void;
  onCloseTab: (id: string) => void;
  onTabUpdate: (id: string, patch: Partial<EditorTab>) => void;
  onSaved: (id: string) => void;
  notify: (tone: 'info' | 'success' | 'error', message: string) => void;
}) {
  const { t } = useI18n();
  const active = tabs.find((tb) => tb.entry.path === activeId) ?? null;
  const [conflict, setConflict] = useState<string | null>(null);
  const [find, setFind] = useState('');
  const [replace, setReplace] = useState('');
  const [findIdx, setFindIdx] = useState(0);
  const [saving, setSaving] = useState(false);

  // Load the active tab's content exactly once.
  useEffect(() => {
    if (!active || active.loaded) return;
    let cancelled = false;
    (async () => {
      try {
        const r = await api.previewText(session.id, active.entry.path);
        if (cancelled) return;
        const content = r.content ? atob(r.content) : '';
        onTabUpdate(active.entry.path, {
          content,
          loaded: true,
          loadedModTime: active.entry.modTime ?? null,
        });
      } catch (e) {
        if (cancelled) return;
        onTabUpdate(active.entry.path, { loaded: true, content: '' });
        notify('error', e instanceof Error ? e.message : t('common.error'));
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active?.entry.path, session.id]);

  const save = useCallback(
    async (force = false) => {
      if (!active) return;
      setSaving(true);
      try {
        if (!force && active.loadedModTime) {
          const r = await api.stat(session.id, active.entry.path);
          const current = r.entry.modTime ?? null;
          if (current && current !== active.loadedModTime) {
            setConflict(active.entry.path);
            return;
          }
        }
        const dir = parentPath(active.entry.path);
        const body = new Blob([active.content], { type: 'text/plain;charset=utf-8' });
        await api.uploadStream(session.id, dir, active.entry.name, body, body.size);
        const r = await api.stat(session.id, active.entry.path);
        onTabUpdate(active.entry.path, {
          dirty: false,
          loadedModTime: r.entry.modTime ?? active.loadedModTime,
          entry: { ...active.entry, modTime: r.entry.modTime },
        });
        onSaved(active.entry.path);
        notify('success', t('editor.saved', { name: active.entry.name }));
      } catch (e) {
        notify('error', e instanceof Error ? e.message : t('common.error'));
      } finally {
        setSaving(false);
      }
    },
    [active, session.id, onTabUpdate, onSaved, notify, t],
  );

  // Ctrl+S saves the active tab.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const meta = e.ctrlKey || e.metaKey;
      const tag = (e.target as HTMLElement)?.tagName;
      if (meta && e.key.toLowerCase() === 's' && active && tag !== 'INPUT') {
        e.preventDefault();
        void save();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active, save]);

  const nextFindIdx = useCallback(
    (dir: 1 | -1): number => {
      if (!active || !find) return -1;
      const hay = active.content;
      const needle = find;
      const lowerHay = hay.toLowerCase();
      const lowerNeedle = needle.toLowerCase();
      let idx: number;
      if (dir === 1) {
        const from = findIdx + needle.length > 0 ? findIdx + needle.length : 0;
        idx = lowerHay.indexOf(lowerNeedle, from);
        if (idx === -1) idx = lowerHay.indexOf(lowerNeedle);
      } else {
        const from = findIdx > 0 ? findIdx - 1 : hay.length;
        idx = lowerHay.lastIndexOf(lowerNeedle, from);
        if (idx === -1) idx = lowerHay.lastIndexOf(lowerNeedle);
      }
      setFindIdx(Math.max(0, idx));
      return idx;
    },
    [active, find, findIdx],
  );

  const findCount = useMemo(() => {
    if (!active || !find) return 0;
    return active.content.toLowerCase().split(find.toLowerCase()).length - 1;
  }, [active, find]);

  const replaceOne = useCallback(() => {
    if (!active || findIdx < 0 || !find) return;
    const next = active.content.slice(0, findIdx) + replace + active.content.slice(findIdx + find.length);
    onTabUpdate(active.entry.path, { content: next, dirty: true });
    setFindIdx(findIdx + replace.length);
  }, [active, find, replace, findIdx, onTabUpdate]);

  const replaceAll = useCallback(() => {
    if (!active || !find) return;
    const re = new RegExp(find.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
    const next = active.content.replace(re, () => replace);
    if (next !== active.content) onTabUpdate(active.entry.path, { content: next, dirty: true });
  }, [active, find, replace, onTabUpdate]);

  const markdownHtml = useMemo(
    () => (active && active.preview ? renderMarkdown(active.content) : ''),
    [active?.preview, active?.content],
  );

  if (!active) return null;

  const lineCount = active.content ? active.content.split('\n').length : 1;

  return (
    <div className="editor" role="region" aria-label={t('editor.title')}>
      <div className="editor-tabs" role="tablist" aria-label={t('editor.tabs')}>
        {tabs.map((tb) => (
          <div
            key={tb.entry.path}
            role="tab"
            aria-selected={tb.entry.path === activeId}
            tabIndex={0}
            className={`editor-tab ${tb.entry.path === activeId ? 'active' : ''}`}
            onClick={() => onSetActive(tb.entry.path)}
            onKeyDown={(e) => e.key === 'Enter' && onSetActive(tb.entry.path)}
            title={tb.entry.path}
          >
            <Icon.Code size={13} />
            <span className="editor-tab-name">
              {tb.entry.name}
              {tb.dirty && <span className="dirty-dot" aria-label={t('editor.dirty')} />}
            </span>
            <button
              className="editor-tab-close"
              aria-label={t('common.close')}
              onClick={(e) => {
                e.stopPropagation();
                if (tb.dirty && !window.confirm(t('editor.discardConfirm', { name: tb.entry.name }))) return;
                onCloseTab(tb.entry.path);
              }}
            >
              <Icon.Close size={12} />
            </button>
          </div>
        ))}
      </div>

      <div className="editor-body">
        {!active.loaded ? (
          <p className="muted" style={{ padding: 20 }}>
            {t('common.loading')}
          </p>
        ) : active.preview ? (
          <div
            className="md-preview"
            // Safe: renderMarkdown escapes all input HTML and only emits
            // whitelisted tags; links are restricted to http(s)/mailto/#.
            // eslint-disable-next-line react/no-danger
            dangerouslySetInnerHTML={{ __html: markdownHtml }}
          />
        ) : (
          <div className="editor-area">
            <div className="line-numbers" aria-hidden="true">
              {Array.from({ length: lineCount }, (_, i) => (
                <div key={i}>{i + 1}</div>
              ))}
            </div>
            <textarea
              className="editor-text mono"
              value={active.content}
              spellCheck={false}
              aria-label={active.entry.path}
              onChange={(e) => onTabUpdate(active.entry.path, { content: e.target.value, dirty: true })}
              onScroll={(e) => {
                const nums = e.currentTarget.parentElement?.querySelector<HTMLElement>('.line-numbers');
                if (nums) nums.scrollTop = e.currentTarget.scrollTop;
              }}
            />
          </div>
        )}
      </div>

      {active.findOpen && (
        <div className="editor-find">
          <input
            className="input"
            style={{ width: 200 }}
            placeholder={t('editor.find')}
            value={find}
            autoFocus
            onChange={(e) => setFind(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                if (nextFindIdx(1) < 0) notify('info', t('editor.findNone'));
              }
            }}
          />
          <span className="muted mono" style={{ fontSize: 11.5 }}>
            {find ? `${findCount} ${t('editor.matches')}` : ''}
          </span>
          <button
            className="btn icon"
            title={t('editor.findPrev')}
            aria-label={t('editor.findPrev')}
            onClick={() => {
              if (nextFindIdx(-1) < 0) notify('info', t('editor.findNone'));
            }}
          >
            <Icon.ArrowUp size={14} />
          </button>
          <button
            className="btn icon"
            title={t('editor.findNext')}
            aria-label={t('editor.findNext')}
            onClick={() => {
              if (nextFindIdx(1) < 0) notify('info', t('editor.findNone'));
            }}
          >
            <Icon.ArrowUp size={14} style={{ transform: 'rotate(180deg)' }} />
          </button>
          <input
            className="input"
            style={{ width: 160 }}
            placeholder={t('editor.replace')}
            value={replace}
            onChange={(e) => setReplace(e.target.value)}
          />
          <button className="btn" onClick={replaceOne} disabled={!find || findIdx < 0}>
            {t('editor.replaceOne')}
          </button>
          <button className="btn" onClick={replaceAll} disabled={!find}>
            {t('editor.replaceAll')}
          </button>
          <button
            className="btn icon"
            title={t('common.close')}
            aria-label={t('common.close')}
            onClick={() => {
              setFind('');
              setReplace('');
              setFindIdx(0);
              onTabUpdate(active.entry.path, { findOpen: false });
            }}
          >
            <Icon.Close size={14} />
          </button>
        </div>
      )}

      <div className="editor-statusbar">
        <span className="muted mono" style={{ fontSize: 11.5 }}>
          {formatBytes(active.content.length)} · {lineCount} {t('editor.lines')}
        </span>
        {active.loadedModTime && (
          <span className="muted" style={{ fontSize: 11.5 }}>
            {t('editor.serverModified', { time: formatDate(active.loadedModTime) })}
          </span>
        )}
        <div className="grow" />
        <label className="radio-row" style={{ fontSize: 12 }}>
          <input
            type="checkbox"
            checked={active.preview}
            disabled={!isMarkdownName(active.entry.name)}
            onChange={(e) => onTabUpdate(active.entry.path, { preview: e.target.checked })}
          />
          {t('editor.preview')}
        </label>
        <button
          className="btn"
          style={{ fontSize: 12, padding: '2px 10px' }}
          onClick={() => onTabUpdate(active.entry.path, { findOpen: !active.findOpen })}
        >
          <Icon.Search size={13} /> {t('editor.find')}
        </button>
        <button
          className="btn primary"
          style={{ fontSize: 12, padding: '2px 10px' }}
          disabled={saving || !active.dirty}
          onClick={() => void save()}
        >
          {saving ? t('common.loading') : t('editor.save')}
        </button>
      </div>

      {conflict === active.entry.path && (
        <div className="modal-backdrop" style={{ zIndex: 60 }}>
          <div className="modal" role="alertdialog" aria-modal="true" aria-label={t('editor.conflictTitle')}>
            <div className="modal-head spread">
              <span>{t('editor.conflictTitle')}</span>
            </div>
            <div className="modal-body">
              <p style={{ marginTop: 0 }}>{t('editor.conflictBody', { name: active.entry.name })}</p>
            </div>
            <div className="modal-foot">
              <button className="btn" data-autofocus="true" onClick={() => setConflict(null)}>
                {t('common.cancel')}
              </button>
              <button className="btn danger" onClick={() => void save(true)}>
                {t('editor.overwrite')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

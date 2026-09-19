import type { Entry } from '../api/client';
import { baseName, joinPath, parentPath } from '../util/format';
import type { Breadcrumb, ClipboardState, ConflictItem, SortState } from './types';

// ---- path helpers ----

export function isHidden(name: string): boolean {
  return name.startsWith('.');
}

/** Normalizes a user-entered path to a clean absolute remote path. */
export function normalizeRemotePath(input: string): string {
  let p = input.trim();
  if (!p) return '/';
  if (!p.startsWith('/')) p = '/' + p;
  p = p.replace(/\/{2,}/g, '/');
  if (p.length > 1 && p.endsWith('/')) p = p.slice(0, -1);
  return p;
}

export function extensionOf(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot <= 0 || dot === name.length - 1) return '';
  return name.slice(dot + 1).toLowerCase();
}

export function buildBreadcrumbs(path: string, homeName: string): Breadcrumb[] {
  const out: Breadcrumb[] = [{ name: homeName, path: '/' }];
  if (!path || path === '/') return out;
  const parts = path.split('/').filter(Boolean);
  let acc = '';
  for (const p of parts) {
    acc += '/' + p;
    out.push({ name: p, path: acc });
  }
  return out;
}

// ---- sorting ----

function typeRank(e: Entry): number {
  if (e.type === 'dir') return 0;
  if (e.type === 'symlink') return 1;
  return 2;
}

export function sortEntries(entries: Entry[], sort: SortState): Entry[] {
  const dir = sort.dir === 'desc' ? -1 : 1;
  const key = sort.key ?? 'name';
  const by = (a: Entry, b: Entry): number => {
    switch (key) {
      case 'size':
        return (a.size - b.size) * dir;
      case 'date':
        return (new Date(a.modTime ?? 0).getTime() - new Date(b.modTime ?? 0).getTime()) * dir;
      case 'permissions':
        return (a.permissions ?? '').localeCompare(b.permissions ?? '') * dir;
      case 'owner':
        return (a.owner ?? '').localeCompare(b.owner ?? '') * dir;
      case 'group':
        return (a.group ?? '').localeCompare(b.group ?? '') * dir;
      case 'type':
        return (a.mimeType ?? a.name).localeCompare(b.mimeType ?? b.name) * dir;
      default:
        return a.name.localeCompare(b.name, undefined, { numeric: true }) * dir;
    }
  };
  return [...entries].sort((a, b) => {
    // Folders always come first, then symlinks, then files.
    const ra = typeRank(a);
    const rb = typeRank(b);
    if (ra !== rb) return ra - rb;
    return by(a, b);
  });
}

// ---- clipboard operation planning ----

export interface PlanResult {
  items: { from: string; to: string }[];
  conflicts: ConflictItem[];
  error?: string;
}

/**
 * Plans a clipboard paste into `destDir`.
 *
 * - `cut` moves; otherwise copies.
 * - For folders: if the destination folder already exists, the source folder
 *   is merged into it (its name is appended to destDir only when it does not
 *   exist) - the same rule as Explorer.
 * - Returns `error` when the operation is impossible (e.g. moving a folder
 *   into its own subtree, or pasting into a path inside the source).
 */
export function planPaste(
  clip: ClipboardState,
  destDir: string,
  destEntries: Entry[],
): PlanResult {
  const existing = new Map<string, Entry>();
  for (const e of destEntries) existing.set(e.path, e);

  const items: { from: string; to: string }[] = [];
  const conflicts: ConflictItem[] = [];

  for (const item of clip.items) {
    const name = baseName(item.path);
    const isDir = item.type === 'dir';
    let to: string;

    if (isDir) {
      const candidate = joinPath(destDir, name);
      const targetExists = existing.has(candidate);
      if (clip.cut) {
        // Moving a folder into its own subtree is invalid.
        if (candidate === item.path) {
          return { items, conflicts, error: 'self' };
        }
        if (candidate.startsWith(item.path + '/') || item.path === destDir) {
          return { items, conflicts, error: 'inside-source' };
        }
        // Destination folder already contains a folder with the same name →
        // the backend will merge; keep the same target.
        to = candidate;
      } else {
        // Copying into a same-named destination folder merges (target name
        // stays the folder's own path); otherwise new folder.
        to = candidate;
        if (targetExists) {
          conflicts.push({
            from: item.path,
            to,
            sourceName: name,
            sourceSize: item.size,
            sourceIsDir: true,
            destName: name,
            destSize: undefined,
          });
        }
      }
    } else {
      to = joinPath(destDir, name);
      if (clip.cut && to === item.path) {
        continue; // no-op: item already at destination
      }
      const dest = existing.get(to);
      if (dest) {
        conflicts.push({
          from: item.path,
          to,
          sourceName: name,
          sourceSize: item.size,
          sourceIsDir: false,
          destName: name,
          destSize: dest.size,
        });
      }
    }

    // Cut of a folder into its own subtree (any depth).
    if (clip.cut && isDir) {
      const destRoot = item.path.endsWith('/') ? item.path : item.path + '/';
      if (destDir === item.path || destDir.startsWith(destRoot)) {
        return { items, conflicts, error: 'inside-source' };
      }
    }

    items.push({ from: item.path, to });
  }

  return { items, conflicts };
}

/**
 * True when `maybeChild` is `folder` itself or inside it.
 */
export function isWithin(folder: string, maybeChild: string): boolean {
  if (!maybeChild) return false;
  const f = folder.endsWith('/') ? folder : folder + '/';
  return maybeChild === folder || maybeChild.startsWith(f);
}

// ---- display ----

export function typeLabel(e: Entry, t: (key: string) => string): string {
  if (e.type === 'dir') return t('fm.typeFolder');
  if (e.isSymlink) return t('files.symlink');
  const ext = extensionOf(e.name);
  switch (ext) {
    case 'txt':
      return t('fm.typeText');
    case 'md':
    case 'markdown':
      return t('fm.typeMarkdown');
    case 'json':
      return t('fm.typeJson');
    case 'yaml':
    case 'yml':
      return t('fm.typeYaml');
    case 'html':
    case 'htm':
      return t('fm.typeHtml');
    case 'log':
      return t('fm.typeLog');
    case 'zip':
    case 'tar':
    case 'gz':
    case 'bz2':
    case 'xz':
      return t('fm.typeArchive');
    case 'jpg':
    case 'jpeg':
    case 'png':
    case 'gif':
    case 'webp':
    case 'svg':
    case 'bmp':
      return t('fm.typeImage');
    case 'pdf':
      return t('fm.typePdf');
    default:
      return e.mimeType ? e.mimeType : t('fm.typeFile');
  }
}

export function fileKilobytes(e: Entry): string {
  return e.type === 'dir' ? '-' : String(e.size);
}

export function parentOf(path: string): string {
  return parentPath(path);
}

export function childPath(dir: string, name: string): string {
  return joinPath(dir, name);
}

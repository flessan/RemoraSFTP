import type { Entry, Favorite } from '../api/client';

export type ViewMode = 'details' | 'list' | 'largeIcons' | 'mediumIcons' | 'smallIcons' | 'compact';

export type ColumnId = 'name' | 'type' | 'size' | 'date' | 'permissions' | 'owner' | 'group';

export interface ColumnState {
  id: ColumnId;
  width: number;
  hidden: boolean;
}

export interface SortState {
  key: ColumnId | null;
  dir: 'asc' | 'desc';
}

export interface Breadcrumb {
  name: string;
  path: string;
}

export interface ClipboardState {
  items: Entry[];
  cut: boolean;
}

export interface ConflictItem {
  from: string;
  to: string;
  sourceName: string;
  sourceSize: number;
  sourceIsDir: boolean;
  destName: string;
  destSize?: number;
}

export type ConflictPolicy = 'replace' | 'skip' | 'rename';

export interface HistoryEntry {
  id: number;
  time: string;
  kind: string;
  message: string;
  undoDescription?: string;
}

export interface SearchOptions {
  query: string;
  startsWith: boolean;
  extension: string;
  caseSensitive: boolean;
  includeHidden: boolean;
  recursive: boolean;
}

export interface SearchState {
  active: boolean;
  running: boolean;
  options: SearchOptions;
  results: Entry[];
  count: number;
  canceled: boolean;
}

export interface ToastLike {
  message: string;
  tone: 'info' | 'success' | 'error';
}

export type PaneKind = 'remote' | 'local';

export interface PaneState {
  kind: PaneKind;
  path: string;
  entries: Entry[];
  loading: boolean;
  error?: string;
}

export function defaultColumns(): ColumnState[] {
  return [
    { id: 'name', width: 340, hidden: false },
    { id: 'type', width: 130, hidden: false },
    { id: 'size', width: 90, hidden: false },
    { id: 'date', width: 170, hidden: false },
    { id: 'permissions', width: 110, hidden: false },
    { id: 'owner', width: 100, hidden: true },
    { id: 'group', width: 100, hidden: true },
  ];
}

export type { Entry, Favorite };

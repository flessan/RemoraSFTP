// Minimal inline SVG icon set (stroke-based, inherits currentColor).
import type { SVGProps } from 'react';

type P = SVGProps<SVGSVGElement> & { size?: number };

function base(size = 18): SVGProps<SVGSVGElement> {
  return {
    width: size,
    height: size,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round',
    strokeLinejoin: 'round',
  };
}

export const Icon = {
  Box: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M21 8l-9-5-9 5v8l9 5 9-5V8z" />
      <path d="M3 8l9 5 9-5M12 13v8" />
    </svg>
  ),
  Files: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
    </svg>
  ),
  Transfers: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M4 7h13M14 4l3 3-3 3M20 17H7M10 14l-3 3 3 3" />
    </svg>
  ),
  Activity: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M22 12h-4l-3 8-5-16-3 8H2" />
    </svg>
  ),
  Shield: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" />
      <path d="M9 12l2 2 4-4" />
    </svg>
  ),
  Settings: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 7 19.4a1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.6 1.6 0 0 0 3 14.6H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 7a1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.6 1.6 0 0 0 10 3V3a2 2 0 1 1 4 0v.1A1.6 1.6 0 0 0 17 4.6l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a2 2 0 1 1 0 4h-.1z" />
    </svg>
  ),
  Plus: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
  Upload: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 16V4m0 0l-4 4m4-4l4 4" />
      <path d="M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2" />
    </svg>
  ),
  Download: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 4v12m0 0l-4-4m4 4l4-4" />
      <path d="M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2" />
    </svg>
  ),
  Refresh: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
      <path d="M21 3v5h-5" />
    </svg>
  ),
  Folder: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  ),
  File: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
    </svg>
  ),
  Link: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M10 13a5 5 0 0 0 7 0l2-2a5 5 0 0 0-7-7l-1 1" />
      <path d="M14 11a5 5 0 0 0-7 0l-2 2a5 5 0 0 0 7 7l1-1" />
    </svg>
  ),
  Trash: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
      <path d="M10 11v6M14 11v6" />
    </svg>
  ),
  Edit: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z" />
    </svg>
  ),
  Copy: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15V5a2 2 0 0 1 2-2h10" />
    </svg>
  ),
  Eye: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  ),
  Close: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M18 6L6 18M6 6l12 12" />
    </svg>
  ),
  Check: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M20 6L9 17l-5-5" />
    </svg>
  ),
  List: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" />
    </svg>
  ),
  Grid: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="3" y="3" width="7" height="7" rx="1" />
      <rect x="14" y="3" width="7" height="7" rx="1" />
      <rect x="3" y="14" width="7" height="7" rx="1" />
      <rect x="14" y="14" width="7" height="7" rx="1" />
    </svg>
  ),
  Search: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <circle cx="11" cy="11" r="7" />
      <path d="M21 21l-4.3-4.3" />
    </svg>
  ),
  Plug: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M9 2v6M15 2v6" />
      <path d="M6 8h12v3a6 6 0 0 1-12 0z" />
      <path d="M12 17v5" />
    </svg>
  ),
  PlugOff: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M9 2v4M15 2v4" />
      <path d="M6 8h12v3a6 6 0 0 1-9 5.2" />
      <path d="M12 17v5M4 4l16 16" />
    </svg>
  ),
  Command: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M18 3a3 3 0 0 0-3 3v12a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V6a3 3 0 1 0-3 3h12" />
    </svg>
  ),
  Server: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="3" y="4" width="18" height="7" rx="2" />
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M7 7.5h.01M7 16.5h.01" />
    </svg>
  ),
  Star: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 3l2.7 5.6 6.1.8-4.5 4.3 1.1 6-5.4-3-5.4 3 1.1-6L3.2 9.4l6.1-.8z" />
    </svg>
  ),
  StarFill: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p} fill="currentColor" stroke="none">
      <path d="M12 3l2.7 5.6 6.1.8-4.5 4.3 1.1 6-5.4-3-5.4 3 1.1-6L3.2 9.4l6.1-.8z" />
    </svg>
  ),
  Home: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M3 11l9-7 9 7v8a2 2 0 0 1-2 2h-4v-6H9v6H5a2 2 0 0 1-2-2z" />
    </svg>
  ),
  Clock: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </svg>
  ),
  ChevronDown: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M6 9l6 6 6-6" />
    </svg>
  ),
  ChevronRight: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M9 6l6 6-6 6" />
    </svg>
  ),
  ArrowLeft: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M19 12H5m0 0l6-6m-6 6l6 6" />
    </svg>
  ),
  ArrowUp: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M12 19V5m0 0l-6 6m6-6l6 6" />
    </svg>
  ),
  Scissors: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <circle cx="6" cy="6" r="3" />
      <circle cx="6" cy="18" r="3" />
      <path d="M20 4L8.1 15.9M14.5 14.5L20 20M8.1 8.1L12 12" />
    </svg>
  ),
  Paste: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" />
      <rect x="8" y="2" width="8" height="4" rx="1" />
    </svg>
  ),
  Code: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <path d="M8 6l-6 6 6 6M16 6l6 6-6 6" />
    </svg>
  ),
  Split: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M12 4v16" />
    </svg>
  ),
  More: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <circle cx="5" cy="12" r="1" fill="currentColor" />
      <circle cx="12" cy="12" r="1" fill="currentColor" />
      <circle cx="19" cy="12" r="1" fill="currentColor" />
    </svg>
  ),
  Column: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M12 4v16M3 9h18" />
    </svg>
  ),
  Stop: ({ size, ...p }: P) => (
    <svg {...base(size)} {...p}>
      <rect x="6" y="6" width="12" height="12" rx="2" />
    </svg>
  ),
};

export function fileIconFor(name: string, type: string, isSymlink = false) {
  if (type === 'dir') return Icon.Folder;
  if (isSymlink) return Icon.Link;
  const ext = name.split('.').pop()?.toLowerCase() ?? '';
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg'].includes(ext)) return Icon.File;
  return Icon.File;
}

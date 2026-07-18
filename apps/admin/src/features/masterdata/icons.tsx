/* Inline SVG icon set for the Master Data page. All icons inherit
   `currentColor` and default to a 20px stroke-based glyph. */
import type { SVGProps } from "react";

type P = SVGProps<SVGSVGElement> & { size?: number };

function Svg({ size = 20, children, ...rest }: P & { children: React.ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...rest}
    >
      {children}
    </svg>
  );
}

export const Icon = {
  database: (p: P) => (
    <Svg {...p}>
      <ellipse cx="12" cy="5" rx="8" ry="3" />
      <path d="M4 5v6c0 1.66 3.58 3 8 3s8-1.34 8-3V5" />
      <path d="M4 11v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6" />
    </Svg>
  ),
  bell: (p: P) => (
    <Svg {...p}>
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </Svg>
  ),
  help: (p: P) => (
    <Svg {...p}>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 2.5-3 4" />
      <line x1="12" y1="17" x2="12" y2="17.01" />
    </Svg>
  ),
  grid: (p: P) => (
    <Svg {...p}>
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </Svg>
  ),
  box: (p: P) => (
    <Svg {...p}>
      <path d="M21 8V16a2 2 0 0 1-1 1.73l-7 4a2 2 0 0 1-2 0l-7-4A2 2 0 0 1 3 16V8a2 2 0 0 1 1-1.73l7-4a2 2 0 0 1 2 0l7 4A2 2 0 0 1 21 8z" />
      <path d="M3.3 7 12 12l8.7-5" />
      <path d="M12 22V12" />
    </Svg>
  ),
  users: (p: P) => (
    <Svg {...p}>
      <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
      <circle cx="9" cy="7" r="4" />
      <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
      <path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </Svg>
  ),
  monitor: (p: P) => (
    <Svg {...p}>
      <rect x="2" y="3" width="20" height="14" rx="2" />
      <line x1="8" y1="21" x2="16" y2="21" />
      <line x1="12" y1="17" x2="12" y2="21" />
    </Svg>
  ),
  cabinet: (p: P) => (
    <Svg {...p}>
      <rect x="4" y="2" width="16" height="20" rx="2" />
      <line x1="4" y1="12" x2="20" y2="12" />
      <line x1="9" y1="7" x2="11" y2="7" />
      <line x1="9" y1="17" x2="11" y2="17" />
    </Svg>
  ),
  pill: (p: P) => (
    <Svg {...p}>
      <path d="M10.5 20.5 3.5 13.5a5 5 0 0 1 7-7l7 7a5 5 0 0 1-7 7z" />
      <path d="m8.5 8.5 7 7" />
    </Svg>
  ),
  folder: (p: P) => (
    <Svg {...p}>
      <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.7-.9L9.6 3.9A2 2 0 0 0 7.9 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2z" />
    </Svg>
  ),
  link: (p: P) => (
    <Svg {...p}>
      <path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1.5 1.5" />
      <path d="M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1.5-1.5" />
    </Svg>
  ),
  check: (p: P) => (
    <Svg {...p}>
      <polyline points="20 6 9 17 4 12" />
    </Svg>
  ),
  checkCircle: (p: P) => (
    <Svg {...p}>
      <circle cx="12" cy="12" r="9" />
      <polyline points="16 9.5 10.8 15 8 12.3" />
    </Svg>
  ),
  search: (p: P) => (
    <Svg {...p}>
      <circle cx="11" cy="11" r="7" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </Svg>
  ),
  chevronDown: (p: P) => (
    <Svg {...p}>
      <polyline points="6 9 12 15 18 9" />
    </Svg>
  ),
  chevronLeft: (p: P) => (
    <Svg {...p}>
      <polyline points="15 18 9 12 15 6" />
    </Svg>
  ),
  chevronRight: (p: P) => (
    <Svg {...p}>
      <polyline points="9 18 15 12 9 6" />
    </Svg>
  ),
  upload: (p: P) => (
    <Svg {...p}>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="8 8 12 4 16 8" />
      <line x1="12" y1="4" x2="12" y2="16" />
    </Svg>
  ),
  download: (p: P) => (
    <Svg {...p}>
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
      <polyline points="8 12 12 16 16 12" />
      <line x1="12" y1="4" x2="12" y2="16" />
    </Svg>
  ),
  plus: (p: P) => (
    <Svg {...p}>
      <line x1="12" y1="5" x2="12" y2="19" />
      <line x1="5" y1="12" x2="19" y2="12" />
    </Svg>
  ),
  sort: (p: P) => (
    <Svg {...p}>
      <path d="M8 4v16" />
      <polyline points="4 8 8 4 12 8" />
      <path d="M16 20V4" />
      <polyline points="20 16 16 20 12 16" />
    </Svg>
  ),
  sliders: (p: P) => (
    <Svg {...p}>
      <line x1="4" y1="7" x2="20" y2="7" />
      <line x1="4" y1="17" x2="20" y2="17" />
      <circle cx="9" cy="7" r="2.4" />
      <circle cx="15" cy="17" r="2.4" />
    </Svg>
  ),
  edit: (p: P) => (
    <Svg {...p}>
      <path d="M11 4H5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h13a2 2 0 0 0 2-2v-6" />
      <path d="M18.5 2.5a2.12 2.12 0 0 1 3 3L12 15l-4 1 1-4z" />
    </Svg>
  ),
  copy: (p: P) => (
    <Svg {...p}>
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </Svg>
  ),
  archive: (p: P) => (
    <Svg {...p}>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" />
      <line x1="10" y1="12" x2="14" y2="12" />
    </Svg>
  ),
  scan: (p: P) => (
    <Svg {...p}>
      <path d="M3 7V5a2 2 0 0 1 2-2h2" />
      <path d="M17 3h2a2 2 0 0 1 2 2v2" />
      <path d="M21 17v2a2 2 0 0 1-2 2h-2" />
      <path d="M7 21H5a2 2 0 0 1-2-2v-2" />
      <line x1="7" y1="12" x2="17" y2="12" />
    </Svg>
  ),
  barcode: (p: P) => (
    <Svg {...p} strokeWidth={1.6}>
      <path d="M4 6v12M7 6v12M9.5 6v12M12 6v12M14.5 6v12M17 6v12M20 6v12" />
    </Svg>
  ),
  hash: (p: P) => (
    <Svg {...p}>
      <line x1="4" y1="9" x2="20" y2="9" />
      <line x1="4" y1="15" x2="20" y2="15" />
      <line x1="10" y1="3" x2="8" y2="21" />
      <line x1="16" y1="3" x2="14" y2="21" />
    </Svg>
  ),
  gauge: (p: P) => (
    <Svg {...p}>
      <path d="M12 14a2 2 0 1 0 0-4 2 2 0 0 0 0 4z" />
      <path d="m13.5 10.5 3-3" />
      <path d="M4 18a9 9 0 1 1 16 0" />
    </Svg>
  ),
  flask: (p: P) => (
    <Svg {...p}>
      <path d="M9 3h6" />
      <path d="M10 3v6.5L5 18a2 2 0 0 0 1.8 3h10.4A2 2 0 0 0 19 18l-5-8.5V3" />
      <path d="M7.5 14h9" />
    </Svg>
  ),
  clock: (p: P) => (
    <Svg {...p}>
      <circle cx="12" cy="12" r="9" />
      <polyline points="12 7 12 12 15 14" />
    </Svg>
  ),
  x: (p: P) => (
    <Svg {...p}>
      <line x1="6" y1="6" x2="18" y2="18" />
      <line x1="6" y1="18" x2="18" y2="6" />
    </Svg>
  ),
  undo: (p: P) => (
    <Svg {...p}>
      <path d="M3 7v6h6" />
      <path d="M3.5 13a9 9 0 1 0 2.2-9.4L3 7" />
    </Svg>
  ),
  logout: (p: P) => (
    <Svg {...p}>
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <polyline points="16 17 21 12 16 7" />
      <line x1="21" y1="12" x2="9" y2="12" />
    </Svg>
  ),
  inventory: (p: P) => (
    <Svg {...p}>
      <path d="M20 5H4a1 1 0 0 0-1 1v3h18V6a1 1 0 0 0-1-1z" />
      <path d="M3 9v9a1 1 0 0 0 1 1h16a1 1 0 0 0 1-1V9" />
      <line x1="9" y1="13" x2="15" y2="13" />
    </Svg>
  ),
};

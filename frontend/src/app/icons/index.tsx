// UI icons in the OT Fabric style (BRANDING.md section 5): 24x24 view box,
// currentColor outline, 1.8 stroke, round caps and joins, no fill. Icons are
// decorative: the control or label next to them carries the name. The set
// starts from the SunSpec Explorer's (v0.0.3) and adds what a trace viewer
// needs.

import type { ReactNode } from 'react';

interface IconProps {
  size?: number;
  className?: string;
}

function icon(paths: ReactNode) {
  return function Icon({ size = 18, className }: IconProps) {
    return (
      <svg
        viewBox="0 0 24 24"
        width={size}
        height={size}
        className={className}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        {paths}
      </svg>
    );
  };
}

// From the SunSpec Explorer.
export const ReadIcon = icon(<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5M5 20h14" />);
export const ExportIcon = icon(<path d="M12 15V4M7.5 8.5 12 4l4.5 4.5M5 14v5h14v-5" />);
export const SearchIcon = icon(
  <>
    <circle cx="11" cy="11" r="6" />
    <path d="m20 20-4.5-4.5" />
  </>,
);
export const ChevronIcon = icon(<path d="m9 6 6 6-6 6" />);
export const WarningIcon = icon(<path d="M12 4 3 19.5h18L12 4ZM12 10v4.5M12 17.2v.3" />);
export const ErrorIcon = icon(
  <>
    <circle cx="12" cy="12" r="8.5" />
    <path d="m9 9 6 6M15 9l-6 6" />
  </>,
);
export const CheckIcon = icon(
  <>
    <circle cx="12" cy="12" r="8.5" />
    <path d="m8.5 12.2 2.4 2.4 4.6-5" />
  </>,
);
export const ClockIcon = icon(
  <>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 7.5V12l3 2" />
  </>,
);
export const CopyIcon = icon(
  <>
    <rect x="8.5" y="8.5" width="11" height="11" rx="2" />
    <path d="M15.5 8.5V6.5a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2" />
  </>,
);
export const SunIcon = icon(
  <>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2.5M12 19.5V22M2 12h2.5M19.5 12H22M4.9 4.9l1.8 1.8M17.3 17.3l1.8 1.8M4.9 19.1l1.8-1.8M17.3 6.7l1.8-1.8" />
  </>,
);
export const MoonIcon = icon(<path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5Z" />);

// Added for the Modbus Trace Explorer.
export const OpenFileIcon = icon(<path d="M4 6.5V18a1.5 1.5 0 0 0 1.5 1.5h13A1.5 1.5 0 0 0 20 18V9a1.5 1.5 0 0 0-1.5-1.5H12L10 5H5.5A1.5 1.5 0 0 0 4 6.5Z" />);
export const DropIcon = icon(<path d="M12 4v11M7.5 8.5 12 4l4.5 4.5M4 15v3.5A1.5 1.5 0 0 0 5.5 20h13a1.5 1.5 0 0 0 1.5-1.5V15" />);
export const FilterIcon = icon(<path d="M4 5h16l-6.5 7.5V19l-3-1.5v-5L4 5Z" />);
export const TimelineIcon = icon(<path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01" />);
export const DeviceIcon = icon(
  <>
    <rect x="4" y="4" width="16" height="7" rx="1.5" />
    <rect x="4" y="13" width="16" height="7" rx="1.5" />
    <path d="M7.5 7.5h.01M7.5 16.5h.01" />
  </>,
);
export const PlugIcon = icon(<path d="M9 7V3M15 7V3M7 7h10v4a5 5 0 0 1-10 0V7ZM12 16v5" />);
export const PlugOffIcon = icon(<path d="M9 7V4M15 7V4M7 7h10v3M7 10v1a5 5 0 0 0 7.5 4.3M12 16v5M4 4l16 16" />);
export const PatternIcon = icon(<path d="M17 3.5 20.5 7 17 10.5M3.5 11V9.5A2.5 2.5 0 0 1 6 7h14.5M7 20.5 3.5 17 7 13.5M20.5 13v1.5A2.5 2.5 0 0 1 18 17H3.5" />);
export const AddressIcon = icon(
  <>
    <rect x="4" y="4" width="16" height="16" rx="1.5" />
    <path d="M9.33 4v16M14.67 4v16M4 9.33h16M4 14.67h16" />
  </>,
);
export const QualityIcon = icon(<path d="M12 3.5 5 6v5.5c0 4.3 3 7.7 7 9 4-1.3 7-4.7 7-9V6l-7-2.5ZM9 12l2.2 2.2L15.5 10" />);
export const InfoIcon = icon(
  <>
    <circle cx="12" cy="12" r="8.5" />
    <path d="M12 11v5.5M12 7.8v.3" />
  </>,
);
export const WriteIcon = icon(<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16v4ZM13.5 6.5l4 4" />);
export const DotIcon = icon(<circle cx="12" cy="12" r="2.5" />);
export const CloseIcon = icon(<path d="m6 6 12 12M18 6 6 18" />);
export const SettingsIcon = icon(
  <>
    <circle cx="12" cy="12" r="3" />
    <path d="M12 3v2.5M12 18.5V21M3 12h2.5M18.5 12H21M5.6 5.6l1.8 1.8M16.6 16.6l1.8 1.8M5.6 18.4l1.8-1.8M16.6 7.4l1.8-1.8" />
  </>,
);
export const FrameIcon = icon(<path d="M5 4h14v16H5V4ZM5 9h14M9 4v5" />);
export const HexIcon = icon(<path d="M8 7 4 12l4 5M16 7l4 5-4 5M13.5 5l-3 14" />);

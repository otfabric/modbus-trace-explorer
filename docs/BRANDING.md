# Branding for the Modbus Trace Explorer

Source of truth: the `otfabric/website` repo (`src/styles/tokens.css`, `src/assets/logo-*.svg`, `public/favicon.svg`, `public/icons/*.svg`, `src/components/site/*`). This document adapts `sunspec-explorer/docs/BRANDING.md` (read from the local checkout on 2026-10-15) to this application; the tokens are identical so that the two OT Fabric GUIs look like one family. Values marked **proposed** do not exist on the website yet and should be added to the website tokens too (the SunSpec Explorer proposes the same four).

## 1. Identity in one line

Slate base, teal "thread" accent, copper "wire" secondary. Dark is the default theme. Product name: **OT Fabric Modbus Trace Explorer**; in the header just **Modbus Trace Explorer**, followed by "powered by [weave mark] **OT** Fabric". Wordmark style "**OT** Fabric" (Barlow Semi Condensed 500, "OT" at 700).

## 2. Tokens (copy into `frontend/src/styles/tokens.css`)

```css
/* Fonts: see frontend/src/styles/fonts.css (section 3). */
@import './fonts.css';

:root {
  --ot-font-display: 'Barlow Semi Condensed', 'Arial Narrow', Arial, sans-serif;
  --ot-font-body: 'IBM Plex Sans', 'Segoe UI', system-ui, sans-serif;
  --ot-font-mono: 'IBM Plex Mono', ui-monospace, Menlo, Consolas, monospace;

  /* Dark (default), from the website */
  --ot-bg: #11181f;
  --ot-surface: #18222c;
  --ot-line: #2a3743;
  --ot-fg: #e6eaee;
  --ot-fg-strong: #ffffff;
  --ot-muted: #94a3b0;
  --ot-accent: #3fc1c6;
  --ot-accent-soft: #123a3d;
  --ot-accent-strong: #dcefef;
  --ot-wire: #e08a4e;
  --ot-header-bg: #18222c;

  /* Proposed additions (not on the website yet) */
  --ot-blue: #8ab0e8;         /* third logo hue; info / third series */
  --ot-danger: #f07178;       /* errors, exceptions */
  --ot-wire-text: #e08a4e;    /* copper usable for small text */
  --ot-control-line: #94a3b0; /* borders of inputs/buttons (3:1 needed) */
}

:root[data-theme='light'] {
  --ot-bg: #ffffff;
  --ot-surface: #f4f6f8;
  --ot-line: #d9dfe4;
  --ot-fg: #2a3743;
  --ot-fg-strong: #17212b;
  --ot-muted: #5a6875;
  --ot-accent: #0a7c80;
  --ot-accent-soft: #dcefef;
  --ot-accent-strong: #0b4f52;
  --ot-wire: #b35a1f;
  --ot-header-bg: #f4f6f8;

  --ot-blue: #2f5d9e;
  --ot-danger: #b42318;
  --ot-wire-text: #9a4a15;    /* #b35a1f is only 4.41:1 on the grey surface */
  --ot-control-line: #5a6875;
}

.ot-label { /* small uppercase mono label: column headers, captions */
  font: 500 0.72rem/1 var(--ot-font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--ot-muted);
}
```

Theme switching is `data-theme` on `:root`. Default dark; follow `prefers-color-scheme` on first run, then remember the user's toggle.

## 3. Typography mapping

| Use | Font |
|---|---|
| App title, window header, section headings, wordmark | Barlow Semi Condensed 500 to 700 |
| Labels, descriptions, buttons, dialogs, summaries in words | IBM Plex Sans 400/500/600 |
| Values, addresses, hex dumps, timestamps, transaction IDs, function codes, units, file names, JSON | IBM Plex Mono 400/500 |
| Column headers, captions | `.ot-label` |

Fonts come from the `@fontsource/*` npm packages, bundled; `fonts.css` declares them by hand with only the `latin` and `latin-ext` subsets, woff2 only, with `unicode-range` (copy the Explorer's file). Never load fonts from a CDN: the application is local only and must work offline. Table density: mono for values (digits align), 13 to 14 px minimum body size.

## 4. Logo and application icon

- The brand mark is the woven fabric: three rounded weft bars (blue, teal, copper) over three vertical warp bars, `viewBox="0 0 48 48"`. Two files swapped by theme like the website's `Logo.astro`: `logo-dark.svg` and `logo-light.svg` (copy from `website/src/assets/`, or from the Explorer's `frontend/src/assets/`, **with the `<metadata>` element stripped**: it is an embedded C2PA manifest of about 7.7 kB).
- **The application's own icon is the protocol's icon** (user rule, 2026-10-14: each OT Fabric GUI is recognized by its protocol's website icon): `website/public/icons/go-modbus.svg` (Modbus: teal tile `#DCEFEF`, stroke `#0A7C80`), copied with the `<metadata>` element stripped to `frontend/src/assets/modbus.svg`. It is used for:
  - the program icon `cmd/modbus-trace-explorer/appicon.png`, rendered by `packaging/icons/render_appicon.py` in the layout macOS expects (rounded tile at 824 px centred on a transparent 1024 px canvas); the macOS bundle derives its `.icns` from it; it is also the window and Dock icon and the Windows resource icon;
  - the header, at 32 px, left of the product name.
- **Header**: protocol icon, then the product name "**Modbus Trace Explorer**" (Barlow Semi Condensed 600, 1.45rem), then a small "powered by [weave mark, 16 px] **OT** Fabric" in the muted color. The weave mark and wordmark are the brand's signature; the product is what the header names. There is no mode badge: the application has one mode.
- The window title is "Modbus Trace Explorer", followed by the open file name.

## 5. Icons

The website has two icon conventions; reuse both. There is no third-party icon library there, so do not add one.

**Project icon** (`public/icons/<repo>.svg`): `viewBox="0 0 48 48"`, rounded tile `rx="11"`, glyph in a 24x24 box at `translate(12 12)`, outline only (`fill="none" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"`), tile tint and stroke paired by domain (general protocol teal `#DCEFEF`/`#0A7C80`; energy copper `#F6E6DA`/`#B35A1F`; transport blue `#E3EAF5`/`#2F5D9E`). This application uses `go-modbus.svg` (general protocol).

**UI icons** (inline SVG components in `frontend/src/app/icons/`): `viewBox="0 0 24 24"`, rendered 18 to 22 px, `stroke="currentColor" stroke-width="1.8"`, round caps and joins, no fill. Icons this application needs, in addition to what the Explorer's set offers (copy and extend it): open-file, drop (upload), filter, timeline (list), device (server), connection (plug), connection-closed (plug-off), pattern (repeat), address (grid), quality (shield-check), export, copy, warning, error, info, clock, read (arrow-down), write (pencil), search, chevron, close, settings, frame (packet), hex. Icon-only buttons need an `aria-label` and a tooltip.

## 6. Color semantics for this application

Brand colors alone do not cover the states this application shows. Never carry meaning by color alone: pair every state with an icon and a text label.

| State | Treatment |
|---|---|
| Primary action, selection, active filter chip | `--ot-accent` (text on `--ot-accent-soft`: `--ot-accent-strong`) |
| Read transaction | neutral text, read (arrow-down) icon |
| **Write** transaction | `--ot-wire-text` copper left border, write (pencil) icon **and** a "W" label |
| Exception response | `--ot-danger`, error icon, code and name in text |
| Unanswered, ambiguous, uncertain, gap, resync, orphan response, guessed role | `--ot-wire-text`, warning icon, text |
| Informational note, second chart series | `--ot-blue`, info icon |
| Value line in the history chart | `--ot-accent`; write markers copper triangles; change markers blue dots |
| Coverage strip | reads: solid accent fill; writes: copper diagonal hatch; both: solid with a copper hatch overlay (pattern, not only color) |
| Dividers, row separators | `--ot-line` (decorative, 1.3:1 by design) |
| Input and button borders | `--ot-control-line` (must reach 3:1) |

## 7. Accessibility numbers (computed 2026-10-13 for the same tokens)

| Pair | Ratio | Verdict |
|---|---|---|
| Dark: fg on bg / muted on surface / accent on surface / wire on surface | 14.8 / 6.2 / 7.4 / 6.1 | pass |
| Dark: blue `#8AB0E8` on surface, danger `#F07178` on surface | 7.3 / 5.6 | pass |
| Light: fg on bg / muted on surface | 12.2 / 5.3 | pass |
| Light: accent `#0A7C80` on surface | 4.61 | passes AA narrowly |
| Light: wire `#B35A1F` on surface `#F4F6F8` | **4.41** | **fails AA for small text**; use `--ot-wire-text` `#9A4A15` (5.76 on surface, 6.24 on white) |
| Light: blue `#2F5D9E`, danger `#B42318` on surface | 6.1 / 6.1 | pass |
| Decorative lines (`--ot-line`) | 1.3 | fine for dividers only |
| Glyph on project tile (graphics, 3:1 needed): teal 4.19, copper 3.92 | | pass |

Re-verify with a script in CI for the pairs this application actually uses (an `a11y` unit test over the token values). Full keyboard navigation, visible focus ring using `--ot-accent`, no information only on hover.

## 8. Website follow-ups (in the `website` repo, only when the user asks)

- Add `modbus-trace-explorer` and `go-modbus-trace` to `src/data/projects.json` (`category` for the GUI like the planned `ui-apps` roadmap entry; `protocols: ["modbus"]`; `icon: "go-modbus"`; `status` `planned` until the repos are public).
- Add the four proposed semantic tokens to `src/styles/tokens.css` so the website and the applications stay in sync.
- Link from `src/content/protocols/modbus.mdx` once released.

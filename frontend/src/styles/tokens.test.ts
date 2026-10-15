import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// A14 and BRANDING.md section 7: colors come from tokens only, and the
// token pairs the application uses keep their contrast.

const css = readFileSync(join(__dirname, 'tokens.css'), 'utf8');

function tokens(theme: 'dark' | 'light'): Record<string, string> {
  const dark = css.slice(css.indexOf(':root {'), css.indexOf(":root[data-theme='light']"));
  const light = css.slice(css.indexOf(":root[data-theme='light']"));
  const read = (block: string) => Object.fromEntries([...block.matchAll(/--(ot-[a-z-]+):\s*(#[0-9a-f]{6})/gi)].map((m) => [m[1] as string, m[2] as string]));
  return theme === 'dark' ? read(dark) : { ...read(dark), ...read(light) };
}

function luminance(hex: string): number {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * (c[0] as number) + 0.7152 * (c[1] as number) + 0.0722 * (c[2] as number);
}

function contrast(a: string, b: string): number {
  const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p) as [number, number];
  return (x + 0.05) / (y + 0.05);
}

describe('tokens', () => {
  for (const theme of ['dark', 'light'] as const) {
    const t = tokens(theme);
    it(`${theme}: text pairs reach WCAG AA (4.5:1), controls 3:1`, () => {
      for (const fg of ['ot-fg', 'ot-muted', 'ot-accent', 'ot-wire-text', 'ot-danger', 'ot-blue']) {
        for (const bg of ['ot-bg', 'ot-surface']) {
          expect(contrast(t[fg] as string, t[bg] as string), `${fg} on ${bg}`).toBeGreaterThanOrEqual(4.5);
        }
      }
      expect(contrast(t['ot-accent-strong'] as string, t['ot-accent-soft'] as string)).toBeGreaterThanOrEqual(4.5);
      expect(contrast(t['ot-control-line'] as string, t['ot-bg'] as string)).toBeGreaterThanOrEqual(3);
    });
  }

  it('no color outside tokens.css', () => {
    const offenders: string[] = [];
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const p = join(dir, name);
        if (statSync(p).isDirectory()) walk(p);
        else if (/\.(tsx?|css)$/.test(name) && !/tokens\.(css|test\.ts)$/.test(name) && /#[0-9a-fA-F]{3,8}\b|rgba?\(|hsla?\(/.test(readFileSync(p, 'utf8'))) offenders.push(p);
      }
    };
    walk(join(__dirname, '..'));
    expect(offenders).toEqual([]);
  });
});

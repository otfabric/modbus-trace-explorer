// A JSON Schema check for the subset docs/schema uses (internal/schemagen:
// $ref into $defs, type, properties, required, additionalProperties, items,
// min/maxItems, minimum, maximum, anyOf, format date-time). Tests only: it lets the
// mock and the engine's fixtures be held to the engine's schemas without a
// validator dependency (A17).

import { readFileSync } from 'node:fs';
import { join } from 'node:path';

type Schema = {
  $ref?: string;
  $defs?: Record<string, Schema>;
  type?: string | string[];
  properties?: Record<string, Schema>;
  required?: string[];
  additionalProperties?: boolean | Schema;
  items?: Schema;
  minItems?: number;
  maxItems?: number;
  minimum?: number;
  maximum?: number;
  anyOf?: Schema[];
  format?: string;
};

// Tests run in frontend/ (npm test, make check).
const SCHEMA_DIR = join(process.cwd(), '..', 'docs', 'schema');

export function loadSchema(name: string): Schema {
  return JSON.parse(readFileSync(join(SCHEMA_DIR, `${name}-1.schema.json`), 'utf8')) as Schema;
}

function typeOf(v: unknown): string {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  if (typeof v === 'number') return Number.isInteger(v) ? 'integer' : 'number';
  return typeof v;
}

/** The problems of v against schema, as "path: problem" lines; [] when it validates. */
export function validate(root: Schema, v: unknown): string[] {
  const out: string[] = [];
  const walk = (s: Schema, x: unknown, path: string) => {
    if (s.$ref) {
      const name = s.$ref.replace('#/$defs/', '');
      const def = root.$defs?.[name];
      if (!def) {
        out.push(`${path}: unknown $ref ${s.$ref}`);
        return;
      }
      walk(def, x, path);
      return;
    }
    if (s.anyOf) {
      // No alternative matches: report the nearest one's problems.
      const typed = (alt: Schema): boolean => {
        const r = alt.$ref ? root.$defs?.[alt.$ref.replace('#/$defs/', '')] : alt;
        const t = r?.type;
        return t === undefined || (Array.isArray(t) ? t : [t]).includes(typeOf(x)) || (typeOf(x) === 'integer' && t === 'number');
      };
      let best: string[] | null = null;
      let bestTyped = false;
      for (const alt of s.anyOf) {
        const before = out.length;
        walk(alt, x, path);
        const problems = out.splice(before);
        if (problems.length === 0) return;
        const isTyped = typed(alt);
        if (!best || (isTyped && !bestTyped) || (isTyped === bestTyped && problems.length < best.length)) {
          best = problems;
          bestTyped = isTyped;
        }
      }
      out.push(...(best ?? [`${path}: matches no alternative`]));
      return;
    }
    if (s.type) {
      const types = Array.isArray(s.type) ? s.type : [s.type];
      const t = typeOf(x);
      if (!types.includes(t) && !(t === 'integer' && types.includes('number'))) {
        out.push(`${path}: ${t}, want ${types.join('|')}`);
        return;
      }
    }
    if (typeof x === 'number' && s.minimum !== undefined && x < s.minimum) out.push(`${path}: ${x} < ${s.minimum}`);
    if (typeof x === 'number' && s.maximum !== undefined && x > s.maximum) out.push(`${path}: ${x} > ${s.maximum}`);
    if (typeof x === 'string' && s.format === 'date-time' && Number.isNaN(Date.parse(x))) out.push(`${path}: not a date-time`);
    if (Array.isArray(x)) {
      if (s.minItems !== undefined && x.length < s.minItems) out.push(`${path}: fewer than ${s.minItems} items`);
      if (s.maxItems !== undefined && x.length > s.maxItems) out.push(`${path}: more than ${s.maxItems} items`);
      if (s.items) x.forEach((item, k) => walk(s.items as Schema, item, `${path}[${k}]`));
    }
    if (typeOf(x) === 'object') {
      const obj = x as Record<string, unknown>;
      for (const r of s.required ?? []) if (!(r in obj)) out.push(`${path}: missing ${r}`);
      for (const [k, val] of Object.entries(obj)) {
        if (val === undefined) continue;
        const ps = s.properties?.[k];
        if (ps) walk(ps, val, `${path}.${k}`);
        else if (s.additionalProperties === false) out.push(`${path}: unexpected ${k}`);
        else if (typeof s.additionalProperties === 'object') walk(s.additionalProperties, val, `${path}.${k}`);
      }
    }
  };
  walk(root, v, '$');
  return out;
}

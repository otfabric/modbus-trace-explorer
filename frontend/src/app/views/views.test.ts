import { describe, expect, it } from 'vitest';
import { compressedAxis, formatValue } from './AddressesView';

const iv = (start: number, end: number) => ({ start, end, count: 1, first_ns: 0, last_ns: 0 });

describe('compressedAxis', () => {
  it('collapses wide empty stretches and keeps near ranges together', () => {
    const { x, spans } = compressedAxis([iv(4096, 4157), iv(100, 100), iv(4160, 4221)], 1000, 10, 64);
    expect(spans).toEqual([
      { start: 100, end: 100 },
      { start: 4096, end: 4221 },
    ]);
    // 127 covered addresses share 990 units; the gap between the spans is 10.
    const unit = 990 / 127;
    expect(x(100)).toBe(0);
    expect(x(101)).toBeCloseTo(unit);
    expect(x(4096)).toBeCloseTo(unit + 10);
    expect(x(4222)).toBeCloseTo(1000);
    expect(x(4000)).toBeCloseTo(unit, 5);
  });

  it('handles nothing and one address', () => {
    expect(compressedAxis([], 100).spans).toEqual([]);
    const one = compressedAxis([iv(7, 7)], 100);
    expect(one.x(7)).toBe(0);
    expect(one.x(8)).toBe(100);
  });
});

describe('formatValue', () => {
  it('formats words as decimal, hex and binary, and bits as 0 or 1', () => {
    expect(formatValue(4660, 'dec', false)).toBe('4660');
    expect(formatValue(4660, 'hex', false)).toBe('0x1234');
    expect(formatValue(4660, 'bin', false)).toBe('0001 0010 0011 0100');
    expect(formatValue(1, 'hex', true)).toBe('1');
  });
});

describe('export and settings helpers', () => {
  it('names exports after the trace and reads port lists', async () => {
    const { baseName } = await import('../components/ExportDialog');
    const { parsePorts } = await import('../components/SettingsDialog');
    expect(baseName('site.pcapng')).toBe('site');
    expect(baseName('site.pcapng.gz')).toBe('site');
    expect(baseName('a.pcap + 2 more')).toBe('a');
    expect(baseName('trace')).toBe('trace');
    expect(parsePorts('502, 1502 503')).toEqual([502, 1502, 503]);
    expect(parsePorts('')).toEqual([]);
    expect(parsePorts('0')).toBeNull();
    expect(parsePorts('70000')).toBeNull();
    expect(parsePorts('x')).toBeNull();
  });
});

describe('byteString and the filter controls', () => {
  it('copies bytes in four formats', async () => {
    const { byteString } = await import('../components/DetailPane');
    const b = ['00', '1F', 'A0'];
    expect(byteString(b, 'spaced')).toBe('00 1F A0');
    expect(byteString(b, 'hex')).toBe('001fa0');
    expect(byteString(b, 'c')).toBe('{ 0x00, 0x1F, 0xA0 }');
    expect(byteString(b, 'python')).toBe('b"\\x00\\x1f\\xa0"');
  });

  it('writes the controls in the filter grammar', async () => {
    const { controlsText } = await import('../components/FilterPopover');
    expect(
      controlsText({ servers: '10.0.0.2:502, 10.0.0.3', units: '1 2', clients: '', conns: '', functions: [3, 4], statuses: ['exception'], classes: [],
        flag: 'slow', notFlag: 'retransmitted', from: '+15m', to: '', space: 'holding', range: '4096-4160' }),
    ).toBe('server:10.0.0.2:502,10.0.0.3 unit:1,2 fc:3,4 status:exception flag:slow -flag:retransmitted from:+15m addr:holding/4096-4160');
    expect(controlsText({ servers: '', units: '', clients: '', conns: '', functions: [], statuses: [], classes: [], flag: '', notFlag: '', from: '', to: '', space: '', range: '' })).toBe('');
  });

  it('reads the mock trace times', async () => {
    const { mockTime } = await import('../service/mock');
    const start = Date.UTC(2026, 9, 14, 12, 2, 11, 384) * 1e6;
    expect(mockTime('+15m')).toBe(start + 900e9);
    expect(mockTime('+1.5s')).toBe(start + 1.5e9);
    expect(mockTime('x')).toBeUndefined();
    expect(mockTime('12:30')).toBeTypeOf('number');
  });
});

describe('the status bar', () => {
  it('names the time zone offset', async () => {
    const { zoneLabel } = await import('../components/StatusJobs');
    expect(zoneLabel(Date.UTC(2026, 9, 14, 12) * 1e6)).toMatch(/^UTC[+-]\d\d:\d\d$/);
  });
});

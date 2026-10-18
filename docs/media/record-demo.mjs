// SPDX-License-Identifier: MIT
//
// Makes the README's screenshots (docs/images) and demo video (docs/media).
// It drives the application built in Wails' server mode (the same binary and
// user interface, served to a browser on localhost) on the demo trace
// testdata/corpus/realistic/site-diagnosis.pcapng, in the local Chrome.
//
//   make demo-server                                # builds bin/modbus-trace-explorer-demo and starts it
//                                                  # on http://localhost:18780 with a scratch home
//   cp docs/media/record-demo.mjs $SCRATCH && cd $SCRATCH && npm i playwright
//   node record-demo.mjs shots out/png              # 2x screenshots
//   node record-demo.mjs video out/vid              # frames + frames.txt for ffmpeg
//   ffmpeg -f concat -safe 0 -i out/vid/frames.txt -vf fps=30,format=yuv420p -c:v libx264 -crf 23 -movflags +faststart demo.mp4
//
// Restart the server before each run: the trace is handed over once, and a
// fresh home starts at the Flows step with an empty cache.
import { chromium } from 'playwright';
import { mkdirSync, writeFileSync } from 'node:fs';

const mode = process.argv[2] ?? 'shots';
const out = process.argv[3];
const url = process.argv[4] ?? 'http://localhost:18780/';
mkdirSync(out, { recursive: true });

const video = mode === 'video';
const browser = await chromium.launch({ channel: 'chrome', headless: true });
const context = await browser.newContext({
  viewport: { width: 1600, height: 900 },
  deviceScaleFactor: video ? 1 : 2,
  colorScheme: 'dark',
});
if (video) {
  // A visible pointer (recorded videos have none) and a caption line.
  await context.addInitScript(() => {
    addEventListener('DOMContentLoaded', () => {
      const dot = document.createElement('div');
      dot.style.cssText = 'position:fixed;z-index:2147483647;width:18px;height:18px;margin:-9px 0 0 -9px;border-radius:50%;' +
        'background:rgba(63,193,198,.35);border:2px solid #3fc1c6;pointer-events:none;transition:transform .08s;left:-40px;top:-40px';
      document.body.appendChild(dot);
      addEventListener('mousemove', (e) => { dot.style.left = e.clientX + 'px'; dot.style.top = e.clientY + 'px'; }, true);
      addEventListener('mousedown', () => { dot.style.transform = 'scale(.7)'; }, true);
      addEventListener('mouseup', () => { dot.style.transform = ''; }, true);
      const cap = document.createElement('div');
      cap.id = 'demo-caption';
      cap.style.cssText = 'position:fixed;z-index:2147483646;left:50%;bottom:44px;transform:translateX(-50%);max-width:980px;' +
        'padding:10px 18px;border-radius:8px;border:1px solid #3fc1c6;background:rgba(9,15,19,.92);color:#e8f1f2;' +
        'font:500 17px/1.4 "IBM Plex Sans",system-ui,sans-serif;text-align:center;pointer-events:none;opacity:0;transition:opacity .35s';
      document.body.appendChild(cap);
    });
  });
}
const page = await context.newPage();
// Video: Chrome's screencast frames with their timestamps; ffmpeg assembles them.
const frames = [];
let cdp;
if (video) {
  cdp = await context.newCDPSession(page);
  cdp.on('Page.screencastFrame', async (f) => {
    frames.push({ t: f.metadata.timestamp, data: f.data });
    await cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {});
  });
}
const pause = (ms) => page.waitForTimeout(video ? ms : Math.min(ms, 600));
const shot = async (name) => {
  if (video) return;
  await page.waitForTimeout(600);
  await page.screenshot({ path: `${out}/${name}.png` });
  console.log('shot', name);
};
const step = async (name, fn) => {
  try {
    await fn();
  } catch (e) {
    console.log('STEP FAILED', name, String(e).split('\n')[0]);
    await page.screenshot({ path: `${out}/failed-${name.replace(/ /g, '-')}.png` }).catch(() => {});
  }
};
// The caption line of the video (nothing in screenshots); '' hides it.
const caption = async (text) => {
  if (!video) return;
  await page.evaluate((t) => {
    const cap = document.getElementById('demo-caption');
    if (!cap) return;
    if (t) cap.innerHTML = t;
    cap.style.opacity = t ? '1' : '0';
  }, text);
};
// Moves the pointer to an element and clicks it, visibly.
const click = async (locator) => {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (box) {
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: video ? 25 : 1 });
    await pause(250);
  }
  await locator.click();
};
const typeInto = async (locator, text) => {
  await click(locator);
  await locator.fill('');
  await locator.pressSequentially(text, { delay: video ? 55 : 0 });
};

const nav = (name) => page.getByRole('navigation', { name: 'Views' }).getByRole('button', { name: new RegExp(name) });
const grid = () => page.getByRole('grid', { name: 'Timeline' });
const rowOf = (table, text, n = 0) => page.getByRole('table', { name: table }).getByRole('row').filter({ hasText: text }).nth(n);

await page.goto(url);
await page.getByRole('button', { name: /^Analyze \d+ flows$/ }).waitFor({ timeout: 30000 });
if (video) await cdp.send('Page.startScreencast', { format: 'png', everyNthFrame: 1 });
await pause(800);

// 1. The scan: the Modbus flows of the trace, proposed.
await caption('Open a pcap or pcapng from tcpdump, Wireshark or dumpcap. The scan proposes the Modbus/TCP flows.');
await pause(5000);
await shot('flows');
await step('analyze', async () => {
  await click(page.getByRole('button', { name: /^Analyze \d+ flows$/ }));
  await grid().waitFor({ timeout: 30000 });
});

// 2. The timeline and a transaction's detail.
await caption('Every request paired with its response: function, registers, result and latency, in time order.');
await pause(2500);
await step('scroll', async () => {
  await page.mouse.move(560, 460, { steps: video ? 20 : 1 });
  await grid().evaluate((el) => el.scrollBy({ top: 1400, behavior: 'smooth' }));
  await pause(1800);
});
await step('detail', async () => {
  await click(grid().getByRole('row').filter({ hasText: '10.40.1.11:502' }).nth(6));
  await pause(1500);
  await click(page.getByRole('tab', { name: 'Response' }));
  await pause(1000);
});
await caption('The detail decodes both PDUs field by field, with their bytes and the packets they came from.');
await pause(3500);
await shot('timeline');

// 3. Use case 1: latency per device.
await caption('<b>Why is the data collection slow?</b> Devices: latency per server and unit.');
await step('devices', async () => {
  await click(nav('Devices'));
  await pause(1800);
  await click(rowOf('Devices', '10.40.1.50:502', 2));
  await pause(2500);
});
await caption('The meters sit behind a Modbus/TCP-to-RTU gateway: answers in 90 to 400 ms over RS-485.');
await pause(3000);
await shot('devices');
await step('offline meter', async () => {
  await click(rowOf('Devices', '10.40.1.50:502', 3));
  await pause(1500);
});
await caption('Meter 4 is offline: every poll waits a full second for the gateway’s exception 0x0B.');
await pause(3500);
await step('show in timeline', async () => {
  await click(page.getByRole('region', { name: 'Device detail' }).getByRole('button', { name: 'Show in timeline' }));
  await pause(1200);
});
await caption('<i>Show in timeline</i> narrows every view to that device: each of its polls, one click away.');
await pause(4000);
await step('reset', async () => {
  await click(page.getByRole('button', { name: 'Reset' }));
  await pause(800);
});

// Connections.
await caption('Conversations: every TCP connection, with its role, duration, gaps and how it ended.');
await step('conversations', async () => {
  await click(nav('Conversations'));
  await pause(1800);
  await click(rowOf('Connections', '51022'));
  await pause(1500);
});
await caption('The EMS reset its connection after six minutes and came back three seconds later.');
await pause(4500);
await shot('conversations');

// 4. Poll intervals and duplicate polling.
await caption('Patterns: what each client polls, how often, and how regularly.');
await step('patterns', async () => {
  await click(nav('Patterns'));
  await pause(2000);
  await click(rowOf('Patterns', '10.40.0.31'));
  await pause(1500);
});
await caption('The EMS reads the inverter’s block every 250 ms, on top of the SCADA’s own 1 s poll.');
await pause(3500);
await shot('patterns');
await step('gateway pattern', async () => {
  await click(rowOf('Patterns', '10.40.1.50:502/1'));
  await pause(1500);
});
await caption('And the SCADA’s 1 s meter cycle stretches to 1.6 s, with jitter: the offline meter costs a second each time.');
await pause(4000);

// 5. Wrong registers: exceptions in the timeline.
await caption('<b>Wrong registers?</b> Filter the timeline: <code>status:exception</code>');
await step('filter', async () => {
  await click(nav('Timeline'));
  await pause(800);
  const f = page.getByRole('textbox', { name: 'Filter' });
  await typeInto(f, 'status:exception');
  await f.press('Enter');
  await pause(1500);
  await click(grid().getByRole('row').filter({ hasText: '40001' }).nth(2));
  await pause(1500);
});
await caption('The SCADA reads the battery at 40001, a 4x address on a 0-based map: illegal data address, every 5 s.');
await pause(4000);
await shot('exceptions');

// 6. Addresses: coverage and the history of one register.
await caption('Addresses: what was read and written, register by register.');
await step('addresses', async () => {
  await click(page.getByRole('button', { name: 'Reset' }));
  await pause(600);
  await click(nav('Addresses'));
  await pause(1200);
  const dev = page.getByLabel('Device');
  const value = await dev.locator('option').filter({ hasText: '10.40.1.30' }).first().getAttribute('value');
  await click(dev);
  await dev.selectOption(value);
  await pause(1500);
  await typeInto(page.getByRole('textbox', { name: 'Address' }), '20');
  await click(page.getByRole('button', { name: 'Show values' }));
  await page.getByRole('img', { name: 'Values over time' }).waitFor({ timeout: 20000 });
  await pause(1500);
});
await caption('The battery’s setpoint over time, with the HMI’s writes marked (the out-of-range one was refused and changed nothing).');
await pause(4500);
await shot('addresses');

// 7. Quality.
await caption('Quality: retransmissions, unanswered requests, resets and the capture’s own facts.');
await step('quality', async () => {
  await click(nav('Quality'));
  await pause(3500);
});
await shot('quality');

// 8. Use case 2: real data for a simulator.
await caption('<b>Real data for a digital twin:</b> export the inverter’s reads as an MBCAP capture.');
await step('export', async () => {
  await click(page.getByRole('button', { name: /^Export$/ }));
  const dialog = page.getByRole('dialog', { name: 'Export' });
  await dialog.waitFor();
  await pause(1500);
  await click(dialog.getByRole('radio', { name: 'One device' }));
  await pause(800);
  const dev = dialog.locator('select[aria-label="Device"]');
  const value = await dev.locator('option').filter({ hasText: '10.40.1.11' }).first().getAttribute('value');
  await click(dev);
  await dev.selectOption(value);
  await pause(1200);
  await typeInto(dialog.getByRole('textbox', { name: /^Label of/ }).first(), 'PV inverter');
  await pause(1500);
});
await caption('The preview says what goes in and why the rest stays out, before anything is written.');
await pause(4000);
await shot('export');
await step('export written', async () => {
  await click(page.getByRole('dialog', { name: 'Export' }).getByRole('button', { name: 'Export…' }));
  await page.getByRole('dialog', { name: 'Export written' }).waitFor({ timeout: 30000 });
  await pause(1500);
});
await caption('Check it, then replay it: <code>modbusctl server replay</code> serves the recorded answers as a simulated device.');
await pause(6000);
await shot('export-written');
await step('done', async () => {
  await click(page.getByRole('dialog', { name: 'Export written' }).getByRole('button', { name: 'Done' }));
  await pause(800);
});

// Screenshots only: the light theme.
if (!video) {
  await step('light', async () => {
    await nav('Timeline').click();
    await page.getByRole('button', { name: /light theme/i }).click();
    await page.waitForTimeout(800);
    await shot('light');
  });
}
await caption('Modbus Trace Explorer: the Modbus-level view of a packet trace. Free and open source.');
await pause(4000);

if (video) {
  await cdp.send('Page.stopScreencast');
  await page.waitForTimeout(300);
  // Frames come only when the page changes: each lasts until the next one.
  const list = [];
  frames.forEach((f, i) => {
    const name = `frame-${String(i).padStart(5, '0')}.png`;
    writeFileSync(`${out}/${name}`, Buffer.from(f.data, 'base64'));
    const next = frames[i + 1] ? frames[i + 1].t : f.t + 1.5;
    list.push(`file '${name}'`, `duration ${Math.max(0.02, next - f.t).toFixed(3)}`);
  });
  list.push(`file 'frame-${String(frames.length - 1).padStart(5, '0')}.png'`);
  writeFileSync(`${out}/frames.txt`, list.join('\n') + '\n');
  console.log('frames', frames.length, 'seconds', (frames.at(-1).t - frames[0].t + 1.5).toFixed(1));
}
await context.close();
await browser.close();
console.log('done');

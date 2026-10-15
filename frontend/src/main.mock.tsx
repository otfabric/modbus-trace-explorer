// Development entry (`npm run dev`): the user interface over the mock
// Service in a browser. `?rows=10000000` sizes the generated trace;
// `?analyzed` opens it as if the cache had it (no import).

import { MockPlatform } from './adapters/mock/platform';
import { mount } from './app/mount';
import { MockService } from './app/service/mock';

const params = new URLSearchParams(window.location.search);
const rows = Number(params.get('rows') ?? '1000000');
mount(new MockService({ rows: Number.isFinite(rows) && rows > 0 ? rows : 1_000_000, analyzed: params.has('analyzed') }), new MockPlatform());

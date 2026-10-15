// Development entry (`npm run dev`): the user interface over the mock
// Service in a browser. `?rows=10000000` sizes the generated trace.

import { MockPlatform } from './adapters/mock/platform';
import { mount } from './app/mount';
import { MockService } from './app/service/mock';

const params = new URLSearchParams(window.location.search);
const rows = Number(params.get('rows') ?? '1000000');
// `?empty` starts without a trace, to see the empty state.
mount(new MockService(Number.isFinite(rows) && rows > 0 ? rows : 1_000_000, params.has('empty')), new MockPlatform());

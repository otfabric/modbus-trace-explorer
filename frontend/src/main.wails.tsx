// Wails entry: the desktop application.

import { WailsPlatform, WailsService } from './adapters/wails/wails';
import { mount } from './app/mount';

mount(new WailsService(), new WailsPlatform());

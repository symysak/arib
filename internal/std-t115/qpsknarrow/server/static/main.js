
"use strict";

import { store } from "./store.js";
import * as link from "./panes/link.js";
import * as control from "./panes/control.js";
import * as quality from "./panes/quality.js";
import * as signal from "./panes/signal.js";
import * as broadcast from "./panes/broadcast.js";
import * as audio from "./panes/audio.js";
import * as inspector from "./panes/inspector.js";
import * as hist from "./panes/hist.js";
import * as system from "./panes/system.js";
import * as log from "./panes/log.js";

for (const pane of [link, control, quality, signal, broadcast, audio, inspector, hist, system, log]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

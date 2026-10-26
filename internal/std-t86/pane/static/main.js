
"use strict";

import { store } from "./store.js";
import { t86 } from "./t86.js";
import * as link from "./panes/link.js";
import * as stations from "./panes/stations.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";
import * as audio from "./panes/audio.js";
import * as timeline from "./panes/timeline.js";
import * as t86bar from "./panes/t86bar.js";
import * as control from "./panes/control.js";
import * as signal from "./panes/signal.js";
import * as broadcast from "./panes/broadcast.js";
import * as system from "./panes/system.js";
import * as inspector from "./panes/inspector.js";
import * as quality from "./panes/quality.js";
import * as hist from "./panes/hist.js";

for (const pane of [link, stations, timeline, recordings, log, settings, audio]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}
for (const pane of [t86bar, control, quality, signal, broadcast, inspector, hist, system]) {
  try { pane.init(t86); } catch (e) { console.error("t86 pane init", e); }
}

store.connect();

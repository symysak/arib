
"use strict";

import { store } from "./store.js";
import "./t61.js";
import * as link from "./panes/link.js";
import * as stations from "./panes/stations.js";
import * as timeline from "./panes/timeline.js";
import * as detail from "./panes/detail.js";
import * as signal from "./panes/signal.js";
import * as quality from "./panes/quality.js";
import * as recordings from "./panes/recordings.js";
import * as l1 from "./panes/l1.js";
import * as l2 from "./panes/l2.js";
import * as l3 from "./panes/l3.js";
import * as calls from "./panes/calls.js";
import * as system from "./panes/system.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";
import * as audio from "./panes/audio.js";

for (const pane of [link, stations, timeline, detail, signal, quality, recordings,
  l1, l2, l3, calls, system, log, settings, audio]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

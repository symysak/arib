
"use strict";

import { store } from "./store.js";
import "./t79.js";
import * as audioSlots from "./panes/audioslots.js";
import * as link from "./panes/link.js";
import * as stations from "./panes/stations.js";
import * as timeline from "./panes/timeline.js";
import * as carrier from "./panes/carrier.js";
import * as signal from "./panes/signal.js";
import * as audio from "./panes/audio.js";
import * as quality from "./panes/quality.js";
import * as calls from "./panes/calls.js";
import * as network from "./panes/network.js";
import * as l1 from "./panes/l1.js";
import * as l2 from "./panes/l2.js";
import * as l3 from "./panes/l3.js";
import * as system from "./panes/system.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";

store.extend({ audio: { mount: audioSlots.mount } });

for (const pane of [link, stations, timeline, carrier, signal, audio, quality, calls, network, l1, l2, l3, system,
  recordings, log, settings]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

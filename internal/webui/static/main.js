
"use strict";

import { store } from "./store.js";
import * as link from "./panes/link.js";
import * as stations from "./panes/stations.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";

for (const pane of [link, stations, recordings, log, settings]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

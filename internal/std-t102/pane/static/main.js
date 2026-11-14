
"use strict";

import { store } from "./store.js";
import "./nxdn.js";
import "./t102.js";
import * as link from "./panes/link.js";
import * as lanes from "./panes/lanes.js";
import * as tx from "./panes/tx.js";
import * as focus from "./panes/focus.js";
import * as audio from "./panes/audio.js";
import * as datamap from "./panes/datamap.js";
import * as datamsg from "./panes/datamsg.js";
import * as l1 from "./panes/l1.js";
import * as l3 from "./panes/l3.js";
import * as eye from "./panes/eye.js";
import * as quality from "./panes/quality.js";
import * as stations from "./panes/stations.js";
import * as detail from "./panes/detail.js";
import * as system from "./panes/system.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";

for (const pane of [link, lanes, tx, focus, audio, datamap, datamsg, l1, l3, eye, quality, stations, detail, system, recordings, log, settings]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

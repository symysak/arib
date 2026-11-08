
"use strict";

import { store } from "./store.js";
import "./t102.js";
import * as link from "./panes/link.js";
import * as stations from "./panes/stations.js";
import * as timeline from "./panes/timeline.js";
import * as detail from "./panes/detail.js";
import * as eye from "./panes/eye.js";
import * as audio from "./panes/audio.js";
import * as tx from "./panes/tx.js";
import * as datamap from "./panes/datamap.js";
import * as datamsg from "./panes/datamsg.js";
import * as quality from "./panes/quality.js";
import * as l1 from "./panes/l1.js";
import * as l3 from "./panes/l3.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";

for (const pane of [link, stations, timeline, detail, eye, audio, tx, datamap, datamsg, quality, l1, l3, recordings, log, settings]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

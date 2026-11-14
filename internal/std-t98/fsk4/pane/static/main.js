
"use strict";

import { store } from "./store.js";
import "./nxdn.js";
import "./t98.js";
import * as link from "./panes/link.js";
import * as channels from "./panes/channels.js";
import * as lanes from "./panes/lanes.js";
import * as tx from "./panes/tx.js";
import * as focus from "./panes/focus.js";
import * as audio from "./panes/audio.js";
import * as l1 from "./panes/l1.js";
import * as l3 from "./panes/l3.js";
import * as eye from "./panes/eye.js";
import * as quality from "./panes/quality.js";
import * as groups from "./panes/groups.js";
import * as radios from "./panes/radios.js";
import * as detail from "./panes/detail.js";
import * as system from "./panes/system.js";
import * as recordings from "./panes/recordings.js";
import * as log from "./panes/log.js";
import * as settings from "./panes/settings.js";

for (const pane of [link, channels, lanes, tx, focus, audio, l1, l3, eye, quality, groups, radios, detail, system, recordings, log, settings]) {
  try { pane.init(store); } catch (e) { console.error("pane init", e); }
}

store.connect();

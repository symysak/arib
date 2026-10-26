
export const KIND = Object.freeze({
  iq: 'iq.wav',
  text: 'txt',
  audio: 'audio.wav',
  audioText: 'audio.txt',
  calls: 'calls.txt',
});

export function encPath(p) {
  return String(p).split('/').map(encodeURIComponent).join('/');
}

export function recURL(api, name, kind) {
  return `api/${api}/${encPath(name)}.${kind}`;
}

const SAFE_EXTERNAL_PROTOCOLS = new Set([
  'http:',
  'https:',
  'mailto:',
  'aionui:',
  'ama:',
  'ccswitch:',
  'cherrystudio:',
  'deepchat:',
  'opencat:',
  'weixin:'
]);

function normalizeExternalURL(value) {
  try {
    const url = new URL(value);
    if (!SAFE_EXTERNAL_PROTOCOLS.has(url.protocol)) return null;
    if ((url.protocol === 'http:' || url.protocol === 'https:') && (url.username || url.password)) {
      return null;
    }
    return url.toString();
  } catch {
    return null;
  }
}

function classifyNavigation(value, appOrigin) {
  let url;
  try {
    url = new URL(value);
  } catch {
    return { action: 'deny' };
  }

  if (url.origin === appOrigin) {
    return { action: 'allow-in-app', url: url.toString() };
  }
  const externalURL = normalizeExternalURL(value);
  if (externalURL) {
    return { action: 'open-external', url: externalURL };
  }
  return { action: 'deny' };
}

module.exports = {
  classifyNavigation,
  normalizeExternalURL
};

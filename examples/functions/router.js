import cf from 'cloudfront';
var kvs = cf.kvs();
async function handler(event) {
  var req = event.request;
  // Permanent redirect for a retired path.
  if (req.uri === '/old-home') {
    return {
      statusCode: 301,
      statusDescription: 'Moved Permanently',
      headers: { location: { value: '/' } },
    };
  }
  // Directory requests resolve to index.html.
  if (req.uri.slice(-1) === '/') {
    req.uri += 'index.html';
  }
  // Expose a feature flag to the origin.
  var variant = 'control';
  try { variant = await kvs.get('homepage-variant'); } catch (e) {}
  req.headers['x-variant'] = { value: variant };
  return req;
}

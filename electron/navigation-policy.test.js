const test = require('node:test');
const assert = require('node:assert/strict');

const { classifyNavigation, normalizeExternalURL } = require('./navigation-policy');

const appOrigin = 'http://127.0.0.1:32123';

test('same-origin navigation stays inside the application', () => {
  assert.deepEqual(classifyNavigation(`${appOrigin}/console?tab=usage`, appOrigin), {
    action: 'allow-in-app',
    url: `${appOrigin}/console?tab=usage`
  });
});

test('external web and allowlisted custom protocols open outside the application', () => {
  assert.deepEqual(classifyNavigation('https://example.com/docs', appOrigin), {
    action: 'open-external',
    url: 'https://example.com/docs'
  });
  assert.deepEqual(classifyNavigation('mailto:support@example.com', appOrigin), {
    action: 'open-external',
    url: 'mailto:support@example.com'
  });
});

test('unsafe, credentialed, and malformed navigation is denied', () => {
  for (const value of [
    'javascript:alert(1)',
    'file:///etc/passwd',
    'https://user:password@example.com/',
    'not a url'
  ]) {
    assert.deepEqual(classifyNavigation(value, appOrigin), { action: 'deny' });
    assert.equal(normalizeExternalURL(value), null);
  }
});

test('a lookalike host is external rather than same-origin', () => {
  assert.deepEqual(classifyNavigation('http://127.0.0.1.example.com:32123/', appOrigin), {
    action: 'open-external',
    url: 'http://127.0.0.1.example.com:32123/'
  });
});

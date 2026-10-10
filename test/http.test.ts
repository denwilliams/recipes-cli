import { test } from 'node:test';
import assert from 'node:assert/strict';
import { setTimeout as delay } from 'node:timers/promises';
import { createFetcher, isPublicAddress, validateUrl, type HttpResponse } from '../src/http.js';
import { RecipesError } from '../src/errors.js';
const url = 'https://www.recipetineats.com/recipe/';
const hosts = ['www.recipetineats.com', 'recipetineats.com'];
const resolvePublic = async () => [{ address: '93.184.216.34', family: 4 }];
const code = (expected: string) => (e: unknown) => e instanceof RecipesError && e.code === expected;
function response(status = 200, headers: HttpResponse['headers'] = { 'content-type': 'text/html; charset=UTF-8' }, chunks = ['<html>Recipe</html>']): HttpResponse {
  return { status, headers, body: (async function* () { for (const chunk of chunks) yield Buffer.from(chunk); })(), close() {} };
}
test('HTTPS exact-host allowlist excludes custom ports, credentials, deceptive hosts and non-URLs', () => {
  assert.equal(validateUrl(`${url}#recipe`, hosts).href, url);
  assert.equal(validateUrl('https://recipetineats.com:443/', hosts).port, '');
  for (const input of ['http://www.recipetineats.com/', 'https://www.recipetineats.com:8443/', 'https://u:p@www.recipetineats.com/', 'https://www.recipetineats.com.evil.example/', 'https://127.0.0.1/', 'file:///etc/passwd', 'bad']) assert.throws(() => validateUrl(input, hosts), code('URL'));
});
test('public address classification excludes IPv4/IPv6 private, loopback, mapped-private, multicast and reserved ranges', () => {
  for (const ip of ['127.0.0.1', '10.1.2.3', '172.16.0.1', '192.168.1.1', '169.254.169.254', '100.64.0.1', '0.0.0.0', '192.0.2.1', '224.0.0.1', '255.255.255.255', '::', '::1', 'fc00::1', 'fe80::1', 'ff02::1', '2001:db8::1', '::ffff:127.0.0.1', 'fe80::1%lo0', 'not-ip']) assert.equal(isPublicAddress(ip), false, ip);
  for (const ip of ['93.184.216.34', '2606:4700::1111', '::ffff:8.8.8.8']) assert.equal(isPublicAddress(ip), true, ip);
});
test('invalid URL fails before DNS; private, mixed and empty DNS fail before transport', async () => {
  let dns = 0, opens = 0;
  const fetch = createFetcher({ resolve: async () => { dns++; return []; }, open: async () => { opens++; return response(); } });
  await assert.rejects(fetch('https://evil.example/', hosts), code('URL'));
  assert.equal(dns, 0);
  for (const addresses of [[], [{ address: '127.0.0.1', family: 4 }], [...await resolvePublic(), { address: '10.0.0.1', family: 4 }]]) {
    await assert.rejects(createFetcher({ resolve: async () => addresses, open: async () => { opens++; return response(); } })(url, hosts), code('NETWORK'));
  }
  assert.equal(opens, 0);
});
test('pins a validated address and closes response; handles UTF8 split chunks', async () => {
  let closed = false;
  const buf = Buffer.from('Riz 🍚');
  const fetch = createFetcher({ resolve: resolvePublic, open: async (requested, address, signal) => {
    assert.equal(requested.href, url); assert.deepEqual(address, (await resolvePublic())[0]); assert.equal(signal.aborted, false);
    return { ...response(), body: (async function* () { yield buf.subarray(0, 5); yield buf.subarray(5); })(), close() { closed = true; } };
  } });
  assert.deepEqual(await fetch(url, hosts), { html: 'Riz 🍚', url });
  assert.equal(closed, true);
});
test('relative same-provider redirects revalidate DNS and attribute final URL', async () => {
  const calls: string[] = []; let dns = 0, closes = 0;
  const fetch = createFetcher({ resolve: async () => { dns++; return resolvePublic(); }, open: async (requested) => {
    calls.push(requested.href);
    const res = calls.length === 1 ? response(302, { location: '/final/' }) : response();
    res.close = () => { closes++; }; return res;
  } });
  const page = await fetch(url, hosts);
  assert.equal(page.url, 'https://www.recipetineats.com/final/'); assert.equal(dns, 2); assert.equal(closes, 2);
});
for (const target of ['https://evil.example/', 'http://www.recipetineats.com/', 'https://127.0.0.1/', 'https://u:p@www.recipetineats.com/', 'https://www.recipetineats.com:444/']) {
  test(`redirect to ${target} is refused before next network call`, async () => {
    let calls = 0;
    await assert.rejects(createFetcher({ resolve: resolvePublic, open: async () => { calls++; return response(302, { location: target }); } })(url, hosts), code('URL'));
    assert.equal(calls, 1);
  });
}
test('DNS turning private after redirect is refused before next transport', async () => {
  let dns = 0, calls = 0;
  await assert.rejects(createFetcher({ resolve: async () => ++dns === 1 ? resolvePublic() : [{ address: '127.0.0.1', family: 4 }], open: async () => { calls++; return response(302, { location: '/final/' }); } })(url, hosts), code('NETWORK'));
  assert.equal(calls, 1);
});
test('redirect loop bound and missing/invalid Location', async () => {
  let calls = 0;
  await assert.rejects(createFetcher({ resolve: resolvePublic, maxRedirects: 2, open: async () => { calls++; return response(302, { location: '/loop/' }); } })(url, hosts), code('REDIRECT'));
  assert.equal(calls, 3);
  for (const headers of [{}, { location: 'https://[' }, { location: ['one', 'two'] }]) {
    await assert.rejects(createFetcher({ resolve: resolvePublic, open: async () => response(301, headers) })(url, hosts), code('REDIRECT'));
  }
});
for (const status of [403, 404, 429, 500, 204]) {
  test(`HTTP ${status} produces actionable error and closes response`, async () => {
    let closed = false;
    const res = response(status); res.close = () => { closed = true; };
    await assert.rejects(createFetcher({ resolve: resolvePublic, open: async () => res })(url, hosts), (e) => code('HTTP')(e) && (e as Error).message.includes(String(status)));
    assert.equal(closed, true);
  });
}
test('body limits apply to declared length and streamed bytes; non-HTML/compression fail clearly', async () => {
  for (const [res, expected] of [
    [response(200, { 'content-type': 'text/html', 'content-length': '99' }), 'SIZE'],
    [response(200, { 'content-type': 'text/html' }, ['1234', '5678']), 'SIZE'],
    [response(200, { 'content-type': 'application/json' }), 'HTTP'],
    [response(200, {}), 'HTTP'],
    [response(200, { 'content-type': 'text/html', 'content-encoding': 'gzip' }), 'HTTP'],
  ] as const) {
    await assert.rejects(createFetcher({ resolve: resolvePublic, maxBytes: 5, open: async () => res })(url, hosts), code(expected));
  }
});
test('DNS, TLS/transport, and body-stream failures become network errors', async () => {
  await assert.rejects(createFetcher({ resolve: async () => { throw new Error('DNS'); } })(url, hosts), code('NETWORK'));
  await assert.rejects(createFetcher({ resolve: resolvePublic, open: async () => { throw new Error('TLS'); } })(url, hosts), code('NETWORK'));
  const res = response(); res.body = (async function* () { throw new Error('truncated'); yield Buffer.from(''); })();
  await assert.rejects(createFetcher({ resolve: resolvePublic, open: async () => res })(url, hosts), code('NETWORK'));
});
test('one timeout bounds DNS, headers and streamed body; no transport after slow DNS', async () => {
  let opens = 0;
  await assert.rejects(createFetcher({ timeoutMs: 10, resolve: async () => { await delay(30); return resolvePublic(); }, open: async () => { opens++; return response(); } })(url, hosts), code('TIMEOUT'));
  await delay(40); assert.equal(opens, 0);
  await assert.rejects(createFetcher({ timeoutMs: 10, resolve: resolvePublic, open: async (_url, _ip, signal) => new Promise((_resolve, reject) => signal.addEventListener('abort', () => reject(new Error('abort')), { once: true })) })(url, hosts), code('TIMEOUT'));
  let closed = false;
  const res = response();
  res.body = (async function* () { await delay(30); yield Buffer.from('late'); })();
  res.close = () => { closed = true; };
  await assert.rejects(createFetcher({ timeoutMs: 10, resolve: resolvePublic, open: async () => res })(url, hosts), code('TIMEOUT'));
  await delay(40); assert.equal(closed, true);
});

test('native HTTPS adapter returns pinned DNS address in both Node lookup modes without sockets', async () => {
  const { createHttpsTransport } = await import('../src/http.js');
  const { EventEmitter } = await import('node:events');
  type RequestFunction = typeof import('node:https').request;
  type RequestOptions = import('node:https').RequestOptions;
  let captured: RequestOptions | undefined;
  let ended = false, reqDestroyed = false, resDestroyed = false;
  const fake = new EventEmitter() as import('node:http').ClientRequest;
  fake.end = (() => { ended = true; return fake; }) as typeof fake.end;
  fake.destroy = (() => { reqDestroyed = true; return fake; }) as typeof fake.destroy;
  const res = new EventEmitter() as import('node:http').IncomingMessage;
  res.statusCode = 200; res.headers = { 'content-type': 'text/html' };
  res.destroy = (() => { resDestroyed = true; return res; }) as typeof res.destroy;
  const fakeRequest = ((requested: URL, options: RequestOptions, callback: (r: import('node:http').IncomingMessage) => void) => {
    assert.equal(requested.hostname, 'www.recipetineats.com');
    captured = options;
    queueMicrotask(() => callback(res));
    return fake;
  }) as RequestFunction;
  const controller = new AbortController();
  const address = { address: '93.184.216.34', family: 4 };
  const result = await createHttpsTransport(fakeRequest)(new URL(url), address, controller.signal);
  assert.equal(ended, true);
  assert.equal(captured?.agent, false);
  assert.equal(captured?.signal, controller.signal);
  assert.equal(captured?.rejectUnauthorized, undefined); // default TLS verification retained
  assert.equal(captured?.checkServerIdentity, undefined);
  assert.match((captured?.headers as Record<string, string>)['User-Agent']!, /^recipes-cli\//);
  assert.equal((captured?.headers as Record<string, string>)['Accept-Encoding'], 'identity');
  captured!.lookup!('www.recipetineats.com', { all: true }, (err, addresses) => {
    assert.equal(err, null); assert.deepEqual(addresses, [address]);
  });
  captured!.lookup!('www.recipetineats.com', { all: false }, (err, ip, family) => {
    assert.equal(err, null); assert.equal(ip, address.address); assert.equal(family, 4);
  });
  result.close(); assert.equal(reqDestroyed, true); assert.equal(resDestroyed, true);
});
test('native HTTPS adapter propagates request errors without sockets', async () => {
  const { createHttpsTransport } = await import('../src/http.js');
  const { EventEmitter } = await import('node:events');
  const fake = new EventEmitter() as import('node:http').ClientRequest;
  fake.end = (() => { queueMicrotask(() => fake.emit('error', new Error('TLS failure'))); return fake; }) as typeof fake.end;
  const requestFn = (() => fake) as typeof import('node:https').request;
  await assert.rejects(createHttpsTransport(requestFn)(new URL(url), { address: '93.184.216.34', family: 4 }, new AbortController().signal), /TLS failure/);
});

// QA reproduction: a residual library category must never permit special/local IPv6.
// Each answer is tested initially, mixed with public DNS, and after a supported redirect.
const nonPublicV6 = [
  '64:ff9b:1::a00:1', // Local-use NAT64, potentially translating to private IPv4
  'fec0::1',          // Deprecated site-local
  '3fff::1',          // Documentation
  '100:0:0:1::1',     // Dummy IPv6 prefix
  '5f00::1',          // Non-globally-reachable SRv6 SID
];
for (const address of nonPublicV6) {
  test(`non-public IPv6 ${address} is classified as refused`, () => {
    assert.equal(isPublicAddress(address), false);
  });
  for (const mixed of [false, true]) {
    test(`${mixed ? 'mixed' : 'initial'} DNS with ${address} rejects before any transport`, async () => {
      let opens = 0;
      const addresses = [{ address, family: 6 }, ...(mixed ? await resolvePublic() : [])];
      await assert.rejects(createFetcher({ resolve: async () => addresses, open: async () => { opens++; return response(); } })(url, hosts), code('NETWORK'));
      assert.equal(opens, 0);
    });
  }
  test(`redirect DNS with ${address} rejects before next transport`, async () => {
    let lookups = 0, opens = 0, closed = false;
    const fetchPage = createFetcher({
      resolve: async () => ++lookups === 1 ? resolvePublic() : [{ address, family: 6 }],
      open: async () => {
        opens++;
        const res = response(302, { location: '/final/' });
        res.close = () => { closed = true; }; return res;
      },
    });
    await assert.rejects(fetchPage(url, hosts), code('NETWORK'));
    assert.equal(lookups, 2); assert.equal(opens, 1); assert.equal(closed, true);
  });
}
test('conservative IPv6 envelope, special blocks, mapped IPv4 and strict literal boundaries', () => {
  for (const address of [
    '1fff:ffff:ffff:ffff:ffff:ffff:ffff:ffff', '4000::', '8000::1',
    '2001::1', '2001:1ff:ffff:ffff:ffff:ffff:ffff:ffff', '2001:db8::',
    '2001:db8:ffff:ffff:ffff:ffff:ffff:ffff', '2002::1', '2002:ffff:ffff:ffff:ffff:ffff:ffff:ffff',
    '2620:4f:8000::1', '3ffe::1', '3fff:fff:ffff:ffff:ffff:ffff:ffff:ffff',
    '64:ff9b::808:808', '64:ff9b:1:ffff:ffff:ffff:ffff:ffff', 'feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff',
    '::ffff:10.0.0.1', '::ffff:192.168.1.1', '::ffff:169.254.169.254',
    '0x08080808', '134744072', '010.0.0.1', '8.8.8',
  ]) assert.equal(isPublicAddress(address), false, address);
  for (const address of [
    '8.8.8.8', '93.184.216.34', '::ffff:8.8.8.8', '::ffff:808:808',
    '2001:4860:4860::8888', '2606:4700::1111', '2a00:1450:4001::1',
    '2001:200::1', '2001:db7:ffff:ffff:ffff:ffff:ffff:ffff', '2001:db9::1',
    '2620:4f:7fff:ffff:ffff:ffff:ffff:ffff', '2620:4f:8001::1',
    '3fff:1000::1', '3ffd:ffff:ffff:ffff:ffff:ffff:ffff:ffff',
  ]) assert.equal(isPublicAddress(address), true, address);
});
test('public IPv6 and mapped public IPv4 still reach pinned transport', async () => {
  for (const address of ['2606:4700::1111', '::ffff:8.8.8.8']) {
    let opens = 0;
    const fetchPage = createFetcher({ resolve: async () => [{ address, family: 6 }], open: async (_url, pinned) => {
      opens++; assert.equal(pinned.address, address); return response();
    } });
    assert.equal((await fetchPage(url, hosts)).url, url); assert.equal(opens, 1);
  }
});

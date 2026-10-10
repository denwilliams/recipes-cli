import { lookup } from 'node:dns/promises';
import { request } from 'node:https';
import { isPublicAddress } from './address-policy.js';
export { isPublicAddress } from './address-policy.js';
import { RecipesError } from './errors.js';

export interface Page { html: string; url: string }
export type FetchPage = (url: string, allowedHosts: readonly string[]) => Promise<Page>;
interface Address { address: string; family: number }
export interface HttpResponse {
  status: number;
  headers: Record<string, string | string[] | undefined>;
  body: AsyncIterable<Uint8Array>;
  close(): void;
}
export interface HttpOptions {
  timeoutMs?: number;
  maxBytes?: number;
  maxRedirects?: number;
  resolve?: (hostname: string) => Promise<Address[]>;
  open?: (url: URL, address: Address, signal: AbortSignal) => Promise<HttpResponse>;
}

/** Exact provider host allowlist, HTTPS only, no credentials or non-default ports. */
export function validateUrl(input: string, hosts: readonly string[]): URL {
  let url: URL;
  try { url = new URL(input); }
  catch { throw new RecipesError('URL', 'Invalid URL. Use a full HTTPS recipe URL.'); }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !hosts.includes(url.hostname)) {
    throw new RecipesError('URL', 'Unsupported URL. Use HTTPS on a supported provider host (no credentials or custom port).');
  }
  url.hash = '';
  return url;
}

export function createHttpsTransport(requestFn: typeof request = request) {
  return async (url: URL, address: Address, signal: AbortSignal): Promise<HttpResponse> => new Promise((resolve, reject) => {
    const req = requestFn(url, {
      signal,
      method: 'GET',
      // Pin the address we validated; TLS still verifies the original hostname.
      lookup: (_hostname, options, callback) => options.all
        ? callback(null, [address])
        : callback(null, address.address, address.family),
      agent: false,
      headers: {
        'User-Agent': 'recipes-cli/0.1.0 (+https://github.com/denwilliams/recipes-cli)',
        Accept: 'text/html',
        'Accept-Encoding': 'identity',
      },
    }, (res) => resolve({
      status: res.statusCode ?? 0,
      headers: res.headers,
      body: res,
      close: () => { res.destroy(); req.destroy(); },
    }));
    req.on('error', reject);
    req.end();
  });
}

/** One deadline covers DNS, all redirects, headers and streamed body. No retries. */
export function createFetcher(options: HttpOptions = {}): FetchPage {
  const timeoutMs = options.timeoutMs ?? 10_000;
  const maxBytes = options.maxBytes ?? 2 * 1024 * 1024;
  const maxRedirects = options.maxRedirects ?? 3;
  const resolve = options.resolve ?? ((host) => lookup(host, { all: true, verbatim: true }));
  const open = options.open ?? createHttpsTransport();
  return async (input, hosts) => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const deadline = new Promise<never>((_, reject) => {
      timer = setTimeout(() => {
        controller.abort();
        reject(new RecipesError('TIMEOUT', `Request timed out after ${timeoutMs}ms. Try again later.`));
      }, timeoutMs);
    });
    const work = async (): Promise<Page> => {
      let url = validateUrl(input, hosts);
      for (let redirects = 0; ; redirects++) {
        const addresses = await resolve(url.hostname);
        controller.signal.throwIfAborted();
        if (!addresses.length || addresses.some((a) => !isPublicAddress(a.address))) {
          throw new RecipesError('NETWORK', 'Provider DNS did not resolve exclusively to public addresses; request refused.');
        }
        const response = await open(url, addresses[0]!, controller.signal);
        try {
          controller.signal.throwIfAborted();
          if ([301, 302, 303, 307, 308].includes(response.status)) {
            if (redirects >= maxRedirects) throw new RecipesError('REDIRECT', 'Too many redirects. Use the final supported recipe URL.');
            const location = response.headers.location;
            if (typeof location !== 'string') throw new RecipesError('REDIRECT', 'Redirect has no valid Location header.');
            let next: URL;
            try { next = new URL(location, url); }
            catch { throw new RecipesError('REDIRECT', 'Redirect contains an invalid URL.'); }
            url = validateUrl(next.href, hosts);
            continue;
          }
          if (response.status !== 200) {
            throw new RecipesError('HTTP', `Provider returned HTTP ${response.status}. Check the URL or try later; access restrictions are not bypassed.`);
          }
          const contentType = response.headers['content-type'];
          if (typeof contentType !== 'string' || !/^text\/html\b|^application\/xhtml\+xml\b/i.test(contentType)) {
            throw new RecipesError('HTTP', 'Provider did not return HTML. Use a recipe page, not an asset or API URL.');
          }
          const encoding = response.headers['content-encoding'];
          if (encoding && encoding !== 'identity') throw new RecipesError('HTTP', 'Provider ignored identity encoding; compressed responses are unsupported.');
          const length = response.headers['content-length'];
          if (typeof length === 'string' && Number(length) > maxBytes) throw new RecipesError('SIZE', `Response exceeds the ${maxBytes}-byte limit.`);
          const chunks: Buffer[] = [];
          let bytes = 0;
          for await (const chunk of response.body) {
            controller.signal.throwIfAborted();
            bytes += chunk.byteLength;
            if (bytes > maxBytes) throw new RecipesError('SIZE', `Response exceeds the ${maxBytes}-byte limit.`);
            chunks.push(Buffer.from(chunk));
          }
          return { html: Buffer.concat(chunks).toString('utf8'), url: url.href };
        } finally { response.close(); }
      }
    };
    try { return await Promise.race([work(), deadline]); }
    catch (error) {
      if (error instanceof RecipesError) throw error;
      if (controller.signal.aborted) throw new RecipesError('TIMEOUT', `Request timed out after ${timeoutMs}ms. Try again later.`);
      throw new RecipesError('NETWORK', 'Could not fetch the provider page. Check your connection/DNS/TLS and try again.');
    } finally { clearTimeout(timer!); }
  };
}

import { ApiClient, ApiError, type SessionStore, type Tokens } from '../src/api/client';

const tok = (n: number): Tokens => ({
  access_token: `a${n}`,
  refresh_token: `r${n}`,
  expires_in: 900,
  user: { id: 'u1', email: 'a@b.c', name: '' },
});

function memStore(initial: Tokens | null): SessionStore & { cur: Tokens | null } {
  const s = {
    cur: initial,
    load: async () => s.cur,
    save: async (t: Tokens) => void (s.cur = t),
    clear: async () => void (s.cur = null),
  };
  return s;
}

const json = (status: number, body?: unknown, headers: Record<string, string> = {}) =>
  new Response(body === undefined ? null : JSON.stringify(body), { status, headers });

function setup(initial: Tokens | null, handler: (url: string, init: RequestInit) => Response | Promise<Response>) {
  const calls: { url: string; init: RequestInit }[] = [];
  const store = memStore(initial);
  const onSignedOut = jest.fn();
  const fetchFn = (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return handler(url, init);
  }) as unknown as typeof fetch;
  const client = new ApiClient({ baseUrl: 'http://x', store, fetchFn, onSignedOut });
  return { client, calls, store, onSignedOut };
}

const authHeader = (init: RequestInit) => (init.headers as Record<string, string>).Authorization;

test('verifyCode saves the session', async () => {
  const { client, store } = setup(null, () => json(200, tok(1)));
  const user = await client.verifyCode('a@b.c', '123456');
  expect(user.email).toBe('a@b.c');
  expect(store.cur?.access_token).toBe('a1');
});

test('sends bearer token from the stored session', async () => {
  const { client, calls } = setup(tok(1), () => json(200, { ok: true }));
  await client.request('GET', '/api/v1/me');
  expect(authHeader(calls[0].init)).toBe('Bearer a1');
});

test('on 401 refreshes once and retries the request', async () => {
  const { client, calls, store } = setup(tok(1), (url, init) => {
    if (url.endsWith('/auth/refresh')) return json(200, tok(2));
    return authHeader(init) === 'Bearer a2' ? json(200, { n: 1 }) : json(401, { error: 'expired' });
  });
  await expect(client.request('GET', '/api/v1/me')).resolves.toEqual({ n: 1 });
  expect(calls.map((c) => c.url.replace('http://x', ''))).toEqual(['/api/v1/me', '/api/v1/auth/refresh', '/api/v1/me']);
  expect(store.cur?.refresh_token).toBe('r2');
});

test('concurrent 401s share a single refresh (refresh tokens are one-time)', async () => {
  const { client, calls } = setup(tok(1), (url, init) => {
    if (url.endsWith('/auth/refresh')) return json(200, tok(2));
    return authHeader(init) === 'Bearer a2' ? json(200, {}) : json(401, { error: 'expired' });
  });
  await Promise.all([client.request('GET', '/a'), client.request('GET', '/b'), client.request('GET', '/c')]);
  expect(calls.filter((c) => c.url.endsWith('/auth/refresh'))).toHaveLength(1);
});

test('rejected refresh signs the user out', async () => {
  const { client, store, onSignedOut } = setup(tok(1), () => json(401, { error: 'no' }));
  await expect(client.request('GET', '/api/v1/me')).rejects.toMatchObject({ status: 401 });
  expect(store.cur).toBeNull();
  expect(onSignedOut).toHaveBeenCalledTimes(1);
});

test('server error during refresh keeps the session', async () => {
  const { client, store, onSignedOut } = setup(tok(1), (url) =>
    url.endsWith('/auth/refresh') ? json(500, { error: 'boom' }) : json(401, { error: 'expired' }),
  );
  await expect(client.request('GET', '/api/v1/me')).rejects.toBeInstanceOf(ApiError);
  expect(store.cur?.refresh_token).toBe('r1');
  expect(onSignedOut).not.toHaveBeenCalled();
});

test('401 from login endpoints does not trigger refresh', async () => {
  const { client, calls } = setup(tok(1), () => json(401, { error: 'invalid or expired code' }));
  await expect(client.verifyCode('a@b.c', '000000')).rejects.toMatchObject({ status: 401, message: 'invalid or expired code' });
  expect(calls).toHaveLength(1);
});

test('network failure becomes ApiError(0)', async () => {
  const { client } = setup(null, () => {
    throw new TypeError('Network request failed');
  });
  await expect(client.requestCode('a@b.c')).rejects.toMatchObject({ status: 0 });
});

test('429 exposes Retry-After', async () => {
  const { client } = setup(null, () => json(429, { error: 'slow down' }, { 'Retry-After': '60' }));
  await expect(client.requestCode('a@b.c')).rejects.toMatchObject({ status: 429, retryAfter: 60 });
});

test('logout clears the session even when the network fails', async () => {
  const { client, store } = setup(tok(1), () => {
    throw new TypeError('offline');
  });
  await client.logout();
  expect(store.cur).toBeNull();
});

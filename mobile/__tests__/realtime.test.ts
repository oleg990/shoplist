import { RealtimeClient, wsUrl, type RealtimeEvent } from '../src/sync/realtime';

class FakeSocket {
  onopen: any = null;
  onmessage: any = null;
  onclose: any = null;
  onerror: any = null;
  sent: string[] = [];
  closed = false;
  send(d: string) {
    this.sent.push(d);
  }
  close() {
    this.closed = true;
  }
}

function setup(token: string | null = 't1') {
  const sockets: FakeSocket[] = [];
  const events: RealtimeEvent[] = [];
  const conn: boolean[] = [];
  const refresh = jest.fn(async () => {
    token = 't2';
  });
  const c = new RealtimeClient({
    url: 'ws://x/api/v1/ws',
    getToken: async () => token,
    refreshToken: refresh,
    onEvent: (e) => events.push(e),
    onConnection: (v) => conn.push(v),
    createSocket: () => {
      const s = new FakeSocket();
      sockets.push(s);
      return s as any;
    },
  });
  return { c, sockets, events, conn, refresh };
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

test('authenticates with the first message and reports events', async () => {
  const { c, sockets, events, conn } = setup();
  c.start();
  await flush();
  sockets[0].onopen({});
  expect(JSON.parse(sockets[0].sent[0])).toEqual({ type: 'auth', token: 't1' });
  sockets[0].onmessage({ data: '{"type":"ready"}' });
  sockets[0].onmessage({ data: '{"type":"list_changed","list_id":"L","kind":"items","version":3}' });
  sockets[0].onmessage({ data: 'garbage' });
  expect(events.map((e) => e.type)).toEqual(['ready', 'list_changed']);
  expect(conn).toEqual([true]);
});

test('reconnects with backoff after a drop', async () => {
  const { c, sockets, conn } = setup();
  c.start();
  await flush();
  sockets[0].onclose({ code: 1006 });
  expect(conn).toEqual([false]);
  expect(sockets).toHaveLength(1);
  jest.advanceTimersByTime(999);
  await flush();
  expect(sockets).toHaveLength(1);
  jest.advanceTimersByTime(1);
  await flush();
  expect(sockets).toHaveLength(2);
  sockets[1].onclose({ code: 1006 });
  jest.advanceTimersByTime(1999);
  await flush();
  expect(sockets).toHaveLength(2);
  jest.advanceTimersByTime(1);
  await flush();
  expect(sockets).toHaveLength(3);
});

test('4401 refreshes the token and reconnects at once with the new one', async () => {
  const { c, sockets, refresh } = setup();
  c.start();
  await flush();
  sockets[0].onclose({ code: 4401 });
  await flush();
  jest.advanceTimersByTime(0);
  await flush();
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(sockets).toHaveLength(2);
  sockets[1].onopen({});
  expect(JSON.parse(sockets[1].sent[0]).token).toBe('t2');
});

test('stop closes the socket and does not reconnect', async () => {
  const { c, sockets } = setup();
  c.start();
  await flush();
  c.stop();
  expect(sockets[0].closed).toBe(true);
  jest.advanceTimersByTime(60000);
  await flush();
  expect(sockets).toHaveLength(1);
});

test('without a session it does not connect', async () => {
  const { c, sockets } = setup(null);
  c.start();
  await flush();
  expect(sockets).toHaveLength(0);
  c.stop();
});

test('wsUrl', () => {
  expect(wsUrl('https://api.example.com/')).toBe('wss://api.example.com/api/v1/ws');
  expect(wsUrl('http://localhost:8080')).toBe('ws://localhost:8080/api/v1/ws');
});

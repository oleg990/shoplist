// Сквозная проверка клиента и движка синхронизации против настоящего сервера.
// Запуск: E2E_API_URL=http://localhost:8080 npx jest e2e (сервер и Postgres должны быть запущены).
import { ApiClient, type SessionStore, type Tokens } from '../src/api/client';
import { makeCatalogApi, makeItemsApi } from '../src/api/items';
import { makeListsApi } from '../src/api/lists';
import { MemoryKv } from '../src/sync/kv';
import { SyncEngine, type Remote } from '../src/sync/engine';
import { RealtimeClient, wsUrl, type RealtimeEvent } from '../src/sync/realtime';

// jest-expo подменяет global fetch заглушкой, поэтому ходим в сеть через node-fetch (транзитивная зависимость Expo).
// eslint-disable-next-line @typescript-eslint/no-require-imports
const realFetch = require('node-fetch') as unknown as typeof fetch;

const URL = process.env.E2E_API_URL;
const run = URL ? describe : describe.skip;

function memStore(): SessionStore {
  let t: Tokens | null = null;
  return { load: async () => t, save: async (x) => void (t = x), clear: async () => void (t = null) };
}

async function login(username: string) {
  const c = new ApiClient({ baseUrl: URL!, store: memStore(), fetchFn: realFetch });
  const t = await c.register(username, 'e2e password 123', 'Тест');
  expect(t.recovery_codes).toHaveLength(8);
  await c.setSession(t);
  return c;
}

function remoteOf(c: ApiClient): Remote {
  const lists = makeListsApi(c);
  const items = makeItemsApi(c);
  return { lists: lists.all, putList: lists.put, renameList: lists.rename, changes: items.changes, put: items.put, patch: items.patch, remove: items.remove, clearBought: items.clearBought };
}

const engineFor = (c: ApiClient) => new SyncEngine(new MemoryKv(), remoteOf(c), () => crypto.randomUUID());

run('against a real server', () => {
  const stamp = Date.now();
  test('login, share a list, sync offline edits between two users', async () => {
    const a = await login(`a${stamp}`);
    const b = await login(`b${stamp}`);
    expect((await a.me()).username).toBe(`a${stamp}`);

    // Список, приглашение, вход второго участника.
    const la = makeListsApi(a);
    const list = await la.create('Дом');
    const inv = await la.invite(list.id);
    expect((await makeListsApi(b).accept(inv.code.toLowerCase())).id).toBe(list.id);
    expect(await la.members(list.id)).toHaveLength(2);

    // Каталог (без входа) и подсказка.
    const found = await makeCatalogApi(a).search('малоко');
    expect(found[0]?.name).toMatch(/Молоко/);

    const ea = engineFor(a);
    const eb = engineFor(b);
    await ea.load();
    await eb.load();
    await ea.sync();
    await eb.sync();
    expect(eb.getLists().map((l) => l.id)).toContain(list.id);

    // A добавляет из каталога и вручную.
    const milk = ea.addItem(list.id, { name: found[0].name, catalog_item_id: found[0].id, category_id: found[0].category_id, unit: found[0].default_unit, quantity: 2 });
    ea.addItem(list.id, { name: 'Что-то своё', price: 99.5 });
    await ea.sync();
    await eb.sync();
    expect(eb.getItems(list.id).map((i) => i.name).sort()).toEqual([found[0].name, 'Что-то своё'].sort());
    const seen = eb.getItems(list.id).find((i) => i.id === milk)!;
    expect(seen.quantity).toBe(2);
    expect(seen.unit).toBe(found[0].default_unit);

    // B отмечает «куплено» и правит, A видит.
    eb.updateItem(list.id, milk, { is_bought: true });
    eb.updateItem(list.id, milk, { quantity: 3, price: 80 });
    await eb.sync();
    await ea.sync();
    expect(ea.getItems(list.id).find((i) => i.id === milk)).toMatchObject({ is_bought: true, quantity: 3, price: 80 });
    expect(ea.getItems(list.id).find((i) => i.id === milk)?.bought_by).not.toBeNull();

    // Очистка купленного видна обоим; свой удалённый элемент исчезает.
    ea.clearBought(list.id);
    await ea.sync();
    await eb.sync();
    expect(eb.getItems(list.id).map((i) => i.name)).toEqual(['Что-то своё']);
    eb.removeItem(list.id, eb.getItems(list.id)[0].id);
    await eb.sync();
    await ea.sync();
    expect(ea.getItems(list.id)).toEqual([]);
    expect(ea.getStatus()).toMatchObject({ online: true, pending: 0, error: '' });

    // B выходит из списка: список пропадает у него после синхронизации.
    await makeListsApi(b).removeMember(list.id, (await b.me()).id);
    await eb.sync();
    expect(eb.getLists().map((l) => l.id)).not.toContain(list.id);

    // После выхода refresh-токен отозван (access-токен живёт до конца своих 15 минут, это ожидаемо).
    const s = (await a.session())!;
    await a.logout();
    const probe = new ApiClient({ baseUrl: URL!, fetchFn: realFetch, store: memStore() });
    await expect(probe.request('POST', '/api/v1/auth/refresh', { refresh_token: s.refresh_token }, false)).rejects.toMatchObject({ status: 401 });
  });

  test('a list created and renamed offline is created once on the real server', async () => {
    const a = await login(`oa${stamp}`);
    const e = engineFor(a);
    await e.load();
    const id = e.createList('Офлайн');
    e.addItem(id, { name: 'Яблоки' });
    e.renameList(id, 'Офлайн 2');
    await e.sync();
    await e.sync(); // повтор: PUT идемпотентен
    expect(e.getStatus()).toMatchObject({ pending: 0, error: '' });
    const all = await makeListsApi(a).all();
    expect(all.filter((l) => l.id === id).map((l) => l.title)).toEqual(['Офлайн 2']);
    expect(e.getItems(id).map((i) => i.name)).toEqual(['Яблоки']);
  });

  test('WebSocket: a change by one member is announced to the other', async () => {
    const a = await login(`wa${stamp}`);
    const b = await login(`wb${stamp}`);
    const la = makeListsApi(a);
    const list = await la.create('Живой');
    await makeListsApi(b).accept((await la.invite(list.id)).code);

    const events: RealtimeEvent[] = [];
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const WS = require('ws');
    const rt = new RealtimeClient({
      url: wsUrl(URL!),
      getToken: async () => (await a.session())?.access_token ?? null,
      refreshToken: async () => void (await a.me()),
      onEvent: (e) => events.push(e),
      createSocket: (u) => new WS(u),
    });
    rt.start();
    const until = async (pred: () => boolean) => {
      for (let i = 0; i < 50 && !pred(); i++) await new Promise((r) => setTimeout(r, 100));
      expect(pred()).toBe(true);
    };
    await until(() => events.some((e) => e.type === 'ready'));

    const eb = engineFor(b);
    await eb.load();
    await eb.sync();
    eb.addItem(list.id, { name: 'Привет' });
    await eb.sync();
    await until(() => events.some((e) => e.type === 'list_changed' && e.list_id === list.id && e.kind === 'items'));
    rt.stop();
  });
});

import { ApiError } from '../src/api/client';
import type { Item, ItemInput, ItemPatch } from '../src/api/items';
import type { ShopList } from '../src/api/lists';
import { MemoryKv } from '../src/sync/kv';
import { SyncEngine, type Remote } from '../src/sync/engine';

// Крошечный «сервер»: общий для нескольких клиентов, как настоящий.
class FakeServer implements Remote {
  items = new Map<string, Item>();
  version = 0;
  down = false;
  reject: ((kind: string) => ApiError | null) | null = null;
  calls: string[] = [];
  listRows: ShopList[] = [
    { id: 'L1', title: 'Дом', owner_id: 'u', role: 'owner', member_count: 1, created_at: '', updated_at: '' },
  ];

  private guard(kind: string) {
    this.calls.push(kind);
    if (this.down) throw new ApiError(0, 'offline');
    const e = this.reject?.(kind);
    if (e) throw e;
  }
  async lists() {
    this.guard('lists');
    return this.listRows;
  }
  async putList(id: string, title: string) {
    this.guard('putList');
    let l = this.listRows.find((x) => x.id === id);
    if (!l) {
      l = { id, title, owner_id: 'u', role: 'owner', member_count: 1, created_at: '', updated_at: '' };
      this.listRows = [l, ...this.listRows];
    }
    return l;
  }
  async renameList(id: string, title: string) {
    this.guard('renameList');
    const l = this.listRows.find((x) => x.id === id);
    if (!l) throw new ApiError(404, 'not found');
    l.title = title;
    return l;
  }
  async changes(listId: string, since: number) {
    this.guard('changes');
    return { items: [...this.items.values()].filter((i) => i.list_id === listId && i.version > since), cursor: this.version };
  }
  async put(listId: string, id: string, b: ItemInput) {
    this.guard('put');
    const it: Item = { ...b, id, list_id: listId, bought_by: null, bought_at: null, version: ++this.version, updated_at: '' };
    this.items.set(id, it);
    return it;
  }
  async patch(listId: string, id: string, b: ItemPatch) {
    this.guard('patch');
    const old = this.items.get(id);
    if (!old || old.deleted) throw new ApiError(404, 'not found');
    const it = { ...old, ...b, version: ++this.version };
    this.items.set(id, it);
    return it;
  }
  async remove(_l: string, id: string) {
    this.guard('delete');
    const old = this.items.get(id);
    if (!old || old.deleted) throw new ApiError(404, 'not found');
    this.items.set(id, { ...old, deleted: true, version: ++this.version });
  }
  async clearBought(listId: string) {
    this.guard('clear');
    for (const i of this.items.values()) {
      if (i.list_id === listId && i.is_bought && !i.deleted) this.items.set(i.id, { ...i, deleted: true, version: ++this.version });
    }
    return {};
  }
}

let n = 0;
const mk = (server: FakeServer, kv = new MemoryKv()) => new SyncEngine(kv, server, () => `id${++n}`);
const names = (e: SyncEngine) => e.getItems('L1').map((i) => i.name);

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

test('items added offline show immediately and reach the server after reconnect', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  await e.sync();
  server.down = true;
  e.addItem('L1', { name: 'Молоко' });
  e.addItem('L1', { name: 'Хлеб' });
  expect(names(e)).toEqual(['Молоко', 'Хлеб']);
  await e.sync();
  expect(e.getStatus().online).toBe(false);
  expect(e.getStatus().pending).toBe(2);
  expect(server.items.size).toBe(0);

  server.down = false;
  await e.sync();
  expect(e.getStatus()).toMatchObject({ online: true, pending: 0 });
  expect([...server.items.values()].map((i) => i.name)).toEqual(['Молоко', 'Хлеб']);
  expect(names(e)).toEqual(['Молоко', 'Хлеб']);
});

test('pending edits survive a restart', async () => {
  const server = new FakeServer();
  const kv = new MemoryKv();
  const a = mk(server, kv);
  await a.load();
  await a.sync();
  server.down = true;
  a.addItem('L1', { name: 'Сыр' });
  await a.flushStorage();

  server.down = false;
  const b = mk(server, kv);
  await b.load();
  expect(names(b)).toEqual(['Сыр']);
  await b.sync();
  expect(server.items.size).toBe(1);
});

test('data cached before going offline is still shown after restart without network', async () => {
  const server = new FakeServer();
  const kv = new MemoryKv();
  const a = mk(server, kv);
  await a.load();
  await server.put('L1', 'x', { catalog_item_id: null, name: 'Яблоки', quantity: null, unit: null, price: null, category_id: null, is_bought: false, position: 1 });
  await a.sync();
  await a.flushStorage();
  server.down = true;
  const b = mk(server, kv);
  await b.load();
  expect(b.getLists()).toHaveLength(1);
  expect(names(b)).toEqual(['Яблоки']);
});

test('edits to an unsent item are merged into the pending put', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  server.down = true;
  const id = e.addItem('L1', { name: 'Чай' });
  e.updateItem('L1', id, { is_bought: true });
  e.updateItem('L1', id, { quantity: 2 });
  expect(e.getStatus().pending).toBe(1);
  server.down = false;
  await e.sync();
  expect(server.calls.filter((c) => c === 'put')).toHaveLength(1);
  expect(server.calls).not.toContain('patch');
  expect(server.items.get(id)).toMatchObject({ is_bought: true, quantity: 2 });
});

test('a change made while its request is in flight is not lost', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  const id = e.addItem('L1', { name: 'Рис' });
  await e.sync();
  // Эмулируем: PATCH ушёл, а пользователь тут же меняет то же поле ещё раз.
  const realPatch = server.patch.bind(server);
  let first = true;
  server.patch = async (l, i, b) => {
    if (first) {
      first = false;
      e.updateItem('L1', id, { is_bought: false });
    }
    return realPatch(l, i, b);
  };
  e.updateItem('L1', id, { is_bought: true });
  await e.sync();
  await e.sync();
  expect(server.items.get(id)?.is_bought).toBe(false);
  expect(e.getItems('L1')[0].is_bought).toBe(false);
});

test('deleting an item removes pending patches and sends delete', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  const id = e.addItem('L1', { name: 'Соль' });
  await e.sync();
  server.down = true;
  e.updateItem('L1', id, { is_bought: true });
  e.removeItem('L1', id);
  expect(names(e)).toEqual([]);
  expect(e.getStatus().pending).toBe(1);
  server.down = false;
  await e.sync();
  expect(server.items.get(id)?.deleted).toBe(true);
});

test("another device's changes arrive by pull, incl. deletions", async () => {
  const server = new FakeServer();
  const a = mk(server);
  const b = mk(server);
  await a.load();
  await b.load();
  const id = a.addItem('L1', { name: 'Масло' });
  await a.sync();
  await b.sync();
  expect(names(b)).toEqual(['Масло']);
  a.updateItem('L1', id, { is_bought: true });
  await a.sync();
  await b.sync();
  expect(b.getItems('L1')[0].is_bought).toBe(true);
  a.clearBought('L1');
  await a.sync();
  await b.sync();
  expect(names(b)).toEqual([]);
});

test('permanently rejected edit is dropped and the server state wins', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  const id = e.addItem('L1', { name: 'Кофе' });
  await e.sync();
  server.items.set(id, { ...server.items.get(id)!, deleted: true, version: ++server.version }); // удалил кто-то другой
  e.updateItem('L1', id, { is_bought: true });
  await e.sync();
  expect(e.getStatus().pending).toBe(0);
  expect(names(e)).toEqual([]);
  expect(e.getStatus().error).not.toBe('');
});

test('deleting an already deleted item is not an error', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  const id = e.addItem('L1', { name: 'Лук' });
  await e.sync();
  server.items.set(id, { ...server.items.get(id)!, deleted: true, version: ++server.version });
  e.removeItem('L1', id);
  await e.sync();
  expect(e.getStatus().error).toBe('');
});

test('server errors (5xx) keep the edit queued', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  server.reject = (k) => (k === 'put' ? new ApiError(503, 'busy') : null);
  e.addItem('L1', { name: 'Вода' });
  await e.sync();
  expect(e.getStatus().pending).toBe(1);
  server.reject = null;
  await e.sync();
  expect(e.getStatus().pending).toBe(0);
});

test('a list that disappeared on the server is forgotten with its pending edits', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  await e.sync();
  server.down = true;
  e.addItem('L1', { name: 'Тест' });
  server.down = false;
  server.listRows = [];
  await e.sync();
  expect(e.getLists()).toHaveLength(0);
  expect(e.getStatus().pending).toBe(0);
});

test('wipe clears everything', async () => {
  const server = new FakeServer();
  const kv = new MemoryKv();
  const e = mk(server, kv);
  await e.load();
  e.addItem('L1', { name: 'X' });
  await e.sync();
  await e.wipe();
  expect(kv.data.size).toBe(0);
  expect(e.getLists()).toHaveLength(0);
});

test('getItems returns a stable reference until something changes', async () => {
  const e = mk(new FakeServer());
  await e.load();
  expect(e.getItems('L1')).toBe(e.getItems('L1'));
});

test('a list created offline with items in it reaches the server in order', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  await e.sync();
  server.down = true;
  const id = e.createList('Дача');
  e.addItem(id, { name: 'Уголь' });
  expect(e.getLists().map((l) => l.title)).toEqual(['Дача', 'Дом']);
  expect(e.getItems(id).map((i) => i.name)).toEqual(['Уголь']);
  await e.sync();
  expect(server.listRows.map((l) => l.id)).not.toContain(id);

  server.down = false;
  server.calls = [];
  await e.sync();
  expect(server.calls.indexOf('putList')).toBeLessThan(server.calls.indexOf('put'));
  expect(server.listRows.map((l) => l.title)).toContain('Дача');
  expect(e.getLists().filter((l) => l.id === id)).toHaveLength(1);
  expect(e.getItems(id).map((i) => i.name)).toEqual(['Уголь']);
  expect(e.getStatus().pending).toBe(0);
});

test('renaming offline updates the title at once and merges into a pending create', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  await e.sync();
  server.down = true;
  e.renameList('L1', 'Квартира');
  expect(e.getLists()[0].title).toBe('Квартира');
  const id = e.createList('Черновик');
  e.renameList(id, 'Дача');
  expect(e.getStatus().pending).toBe(2);

  server.down = false;
  await e.sync();
  expect(server.listRows.map((l) => l.title).sort()).toEqual(['Дача', 'Квартира']);
});

test('a list created offline survives a restart', async () => {
  const server = new FakeServer();
  const kv = new MemoryKv();
  const a = mk(server, kv);
  await a.load();
  await a.sync();
  server.down = true;
  const id = a.createList('Праздник');
  await a.flushStorage();

  server.down = false;
  const b = mk(server, kv);
  await b.load();
  expect(b.getLists().map((l) => l.id)).toContain(id);
  await b.sync();
  expect(server.listRows.map((l) => l.id)).toContain(id);
});

test('a rejected list create is dropped and the list disappears', async () => {
  const server = new FakeServer();
  const e = mk(server);
  await e.load();
  await e.sync();
  server.reject = (k) => (k === 'putList' ? new ApiError(409, 'list id is taken') : null);
  const id = e.createList('Чужой');
  await e.sync();
  expect(e.getLists().map((l) => l.id)).not.toContain(id);
  expect(e.getStatus().pending).toBe(0);
});

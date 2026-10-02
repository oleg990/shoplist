import type { Item, ItemInput, ItemPatch } from '../api/items';
import type { ShopList } from '../api/lists';
import { ApiError } from '../api/client';
import { applyChanges, nextPosition, sortItems } from '../items/merge';
import type { KeyValueStore } from './kv';

// Очередь неотправленных правок позиций. Каждая правка идемпотентна на сервере
// (PUT с id клиента, PATCH, DELETE), поэтому повторная отправка после обрыва безопасна.
export type Op =
  | { seq: number; kind: 'put'; listId: string; itemId: string; body: ItemInput }
  | { seq: number; kind: 'patch'; listId: string; itemId: string; body: ItemPatch }
  | { seq: number; kind: 'delete'; listId: string; itemId: string }
  | { seq: number; kind: 'clear'; listId: string };

export interface Remote {
  lists(): Promise<ShopList[]>;
  changes(listId: string, since: number): Promise<{ items: Item[]; cursor: number }>;
  put(listId: string, id: string, body: ItemInput): Promise<Item>;
  patch(listId: string, id: string, body: ItemPatch): Promise<Item>;
  remove(listId: string, id: string): Promise<void>;
  clearBought(listId: string): Promise<unknown>;
}

export type SyncStatus = { online: boolean; syncing: boolean; pending: number; error: string };

type Persisted = { ops: Op[]; seq: number };
type ListData = { items: Item[]; cursor: number };

const K_LISTS = 'lists';
const K_OPS = 'ops';
const kItems = (listId: string) => `items:${listId}`;

// Сетевая/серверная ошибка: правку оставляем в очереди и пробуем позже.
const isTransient = (e: unknown) => !(e instanceof ApiError) || e.status === 0 || e.status >= 500 || e.status === 401 || e.status === 429;

export class SyncEngine {
  private lists: ShopList[] = [];
  // Подтверждённое сервером состояние; то, что видит пользователь, = base + очередь правок.
  private base = new Map<string, ListData>();
  private ops: Op[] = [];
  private seq = 0;
  private listeners = new Set<() => void>();
  private viewCache = new Map<string, Item[]>();
  private status: SyncStatus = { online: true, syncing: false, pending: 0, error: '' };
  private running: Promise<void> | null = null;
  private rerun = false;
  private inflight = 0; // seq правки, которая сейчас отправляется
  private timer: ReturnType<typeof setTimeout> | null = null;
  private saving: Promise<void> = Promise.resolve();
  private disposed = false;

  constructor(
    private kv: KeyValueStore,
    private remote: Remote,
    private newId: () => string,
    private now: () => string = () => new Date().toISOString(),
  ) {}

  // ---- чтение ----

  async load() {
    this.lists = parse<ShopList[]>(await this.kv.get(K_LISTS), []);
    const p = parse<Persisted>(await this.kv.get(K_OPS), { ops: [], seq: 0 });
    this.ops = p.ops;
    this.seq = p.seq;
    for (const l of this.lists) this.base.set(l.id, parse<ListData>(await this.kv.get(kItems(l.id)), { items: [], cursor: 0 }));
    this.changed();
  }

  getLists = () => this.lists;
  getStatus = () => this.status;

  // Возвращает один и тот же массив, пока ничего не изменилось (нужно для useSyncExternalStore).
  getItems = (listId: string): Item[] => {
    let v = this.viewCache.get(listId);
    if (!v) {
      v = sortItems(this.view(listId));
      this.viewCache.set(listId, v);
    }
    return v;
  };

  subscribe = (fn: () => void) => {
    this.listeners.add(fn);
    return () => void this.listeners.delete(fn);
  };

  private view(listId: string): Item[] {
    let items = [...(this.base.get(listId)?.items ?? [])];
    for (const op of this.ops) {
      if (op.listId !== listId) continue;
      if (op.kind === 'put') {
        const i = items.findIndex((x) => x.id === op.itemId);
        const old = i >= 0 ? items[i] : undefined;
        const item: Item = {
          ...op.body,
          id: op.itemId,
          list_id: listId,
          bought_by: old?.bought_by ?? null,
          bought_at: old?.bought_at ?? null,
          version: old?.version ?? 0,
          updated_at: old?.updated_at ?? this.now(),
        };
        if (i >= 0) items[i] = item;
        else items.push(item);
      } else if (op.kind === 'patch') {
        items = items.map((x) => (x.id === op.itemId ? { ...x, ...op.body } : x));
      } else if (op.kind === 'delete') {
        items = items.filter((x) => x.id !== op.itemId);
      } else {
        items = items.filter((x) => !x.is_bought);
      }
    }
    return items;
  }

  private changed() {
    this.viewCache.clear();
    this.status = { ...this.status, pending: this.ops.length };
    this.listeners.forEach((l) => l());
  }

  // ---- правки пользователя (работают без сети) ----

  addItem(listId: string, input: Partial<ItemInput> & { name: string }): string {
    const itemId = this.newId();
    const body: ItemInput = {
      catalog_item_id: null,
      quantity: null,
      unit: null,
      price: null,
      category_id: null,
      is_bought: false,
      position: nextPosition(this.view(listId)),
      ...input,
    };
    this.enqueue({ kind: 'put', listId, itemId, body });
    return itemId;
  }

  updateItem(listId: string, itemId: string, patch: ItemPatch) {
    // Правка ещё не отправленной позиции или ещё не отправленной правки сливается с ней.
    const last = this.lastOpFor(itemId);
    if (last && (last.kind === 'put' || last.kind === 'patch') && last.seq !== this.inflight) {
      last.body = { ...last.body, ...patch } as never;
      this.persistOps();
      this.changed();
      this.requestSync();
      return;
    }
    this.enqueue({ kind: 'patch', listId, itemId, body: patch });
  }

  removeItem(listId: string, itemId: string) {
    // Неотправленные правки патчами теряют смысл; put оставляем: он мог уже дойти до сервера.
    this.ops = this.ops.filter((o) => !(o.kind === 'patch' && o.itemId === itemId && o.seq !== this.inflight));
    this.enqueue({ kind: 'delete', listId, itemId });
  }

  clearBought(listId: string) {
    this.enqueue({ kind: 'clear', listId });
  }

  private lastOpFor(itemId: string): Op | undefined {
    for (let i = this.ops.length - 1; i >= 0; i--) {
      const o = this.ops[i];
      if ('itemId' in o && o.itemId === itemId) return o;
      if (o.kind === 'clear') return undefined;
    }
    return undefined;
  }

  private enqueue(op: DistributiveOmit<Op, 'seq'>) {
    this.ops.push({ ...op, seq: ++this.seq } as Op);
    this.persistOps();
    this.changed();
    this.requestSync();
  }

  // ---- синхронизация ----

  // Откладывает синхронизацию, чтобы несколько быстрых правок ушли одной серией.
  requestSync(delayMs = 300) {
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.sync();
    }, delayMs);
  }

  dispose() {
    this.disposed = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.listeners.clear();
  }

  // Одна синхронизация за раз; если во время неё попросили ещё одну, она выполнится следом.
  sync(): Promise<void> {
    if (this.running) {
      this.rerun = true;
      return this.running;
    }
    this.running = (async () => {
      try {
        do {
          this.rerun = false;
          await this.syncOnce();
        } while (this.rerun);
      } finally {
        this.running = null;
        this.setStatus({ syncing: false });
      }
    })();
    return this.running;
  }

  private setStatus(p: Partial<SyncStatus>) {
    this.status = { ...this.status, ...p };
    this.listeners.forEach((l) => l());
  }

  private async syncOnce() {
    this.setStatus({ syncing: true, error: '' });
    try {
      await this.flush();
      await this.pullLists();
      for (const l of [...this.lists]) {
        try {
          await this.pullItems(l.id);
        } catch (e) {
          if (isTransient(e)) throw e;
          if (e instanceof ApiError && e.status === 404) this.dropList(l.id);
        }
      }
      this.setStatus({ online: true });
    } catch (e) {
      if (isTransient(e)) this.setStatus({ online: false });
      else this.setStatus({ error: 'Не удалось синхронизировать часть данных.' });
    }
  }

  private async flush() {
    while (this.ops.length > 0) {
      const op = this.ops[0];
      this.inflight = op.seq;
      try {
        await this.send(op);
      } catch (e) {
        if (isTransient(e)) throw e;
        // Сервер отверг правку окончательно (список удалён, позиции нет и т. п.):
        // убираем её, дальше pull вернёт правду. Удаление уже удалённого — не ошибка.
        if (!(op.kind === 'delete' && e instanceof ApiError && e.status === 404)) {
          this.setStatus({ error: 'Часть изменений не удалось применить.' });
        }
      } finally {
        this.inflight = 0;
      }
      this.ops = this.ops.filter((o) => o.seq !== op.seq);
      this.persistOps();
      this.changed();
    }
  }

  private async send(op: Op) {
    switch (op.kind) {
      case 'put':
        return this.accept(op.listId, await this.remote.put(op.listId, op.itemId, op.body));
      case 'patch':
        return this.accept(op.listId, await this.remote.patch(op.listId, op.itemId, op.body));
      case 'delete':
        return this.remote.remove(op.listId, op.itemId);
      case 'clear':
        return this.remote.clearBought(op.listId);
    }
  }

  private accept(listId: string, item: Item) {
    const d = this.data(listId);
    d.items = applyChanges(d.items, [item]);
    this.persistList(listId);
  }

  private data(listId: string): ListData {
    let d = this.base.get(listId);
    if (!d) {
      d = { items: [], cursor: 0 };
      this.base.set(listId, d);
    }
    return d;
  }

  private async pullLists() {
    const fresh = await this.remote.lists();
    const ids = new Set(fresh.map((l) => l.id));
    for (const l of this.lists) {
      if (!ids.has(l.id)) this.forget(l.id);
    }
    this.lists = fresh;
    this.saveLists();
    this.changed();
  }

  private saveLists() {
    this.queueSave(() => this.kv.set(K_LISTS, JSON.stringify(this.lists)));
  }

  private dropList(listId: string) {
    this.lists = this.lists.filter((l) => l.id !== listId);
    this.forget(listId);
    this.saveLists();
    this.changed();
  }

  // Список пропал (удалён или пользователь вышел из него): стираем его данные и правки.
  private forget(listId: string) {
    this.base.delete(listId);
    this.ops = this.ops.filter((o) => o.listId !== listId);
    this.queueSave(() => this.kv.remove(kItems(listId)));
    this.persistOps();
  }

  async pullItems(listId: string) {
    const d = this.data(listId);
    const r = await this.remote.changes(listId, d.cursor);
    d.items = applyChanges(d.items, r.items);
    d.cursor = r.cursor;
    this.persistList(listId);
    this.changed();
  }

  // Событие WebSocket: забрать изменения одного списка.
  async onListChanged(listId: string) {
    if (this.running) {
      this.rerun = true;
      return;
    }
    try {
      await this.pullItems(listId);
      this.setStatus({ online: true });
    } catch (e) {
      if (isTransient(e)) this.setStatus({ online: false });
    }
  }

  // ---- сохранение ----

  private persistOps() {
    this.queueSave(() => this.kv.set(K_OPS, JSON.stringify({ ops: this.ops, seq: this.seq } satisfies Persisted)));
  }

  private persistList(listId: string) {
    const d = this.base.get(listId);
    if (d) this.queueSave(() => this.kv.set(kItems(listId), JSON.stringify(d)));
  }

  private queueSave(fn: () => Promise<void>) {
    if (this.disposed) return;
    this.saving = this.saving.then(fn).catch(() => {});
  }

  // Ждёт, пока всё записано (для тестов и выхода из аккаунта).
  flushStorage() {
    return this.saving;
  }

  async wipe() {
    this.dispose();
    await this.saving;
    this.lists = [];
    this.base.clear();
    this.ops = [];
    await this.kv.clear();
    this.changed();
  }
}

type DistributiveOmit<T, K extends keyof never> = T extends unknown ? Omit<T, K> : never;

function parse<T>(raw: string | null, fallback: T): T {
  if (!raw) return fallback;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

import * as Crypto from 'expo-crypto';
import { useCallback, useEffect, useRef, useState } from 'react';
import { itemsApi, type Item, type ItemInput, type ItemPatch } from '../api/items';
import { errorMessage } from '../errors';
import { applyChanges, nextPosition, sortItems } from './merge';

// Позиции списка. Сейчас — онлайн с оптимистичным обновлением;
// офлайн-хранилище и синхронизация заменят внутренности, не меняя интерфейс.
export function useListItems(listId: string) {
  const [items, setItems] = useState<Item[]>([]);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const cursor = useRef(0);
  const itemsRef = useRef<Item[]>([]);
  itemsRef.current = items;

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const r = await itemsApi.changes(listId, cursor.current);
      cursor.current = r.cursor;
      setItems((cur) => applyChanges(cur, r.items));
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setRefreshing(false);
    }
  }, [listId]);

  useEffect(() => {
    cursor.current = 0;
    setItems([]);
    refresh();
  }, [refresh]);

  // Применяет ответ сервера, не затирая более новую версию.
  const accept = (server: Item) => setItems((cur) => applyChanges(cur, [server]));
  const fail = (e: unknown) => {
    setError(errorMessage(e));
    refresh(); // вернуть правду с сервера после неудачной правки
  };

  const add = useCallback(
    async (input: Partial<ItemInput> & { name: string }) => {
      const id = Crypto.randomUUID();
      const full: ItemInput = {
        catalog_item_id: null,
        quantity: null,
        unit: null,
        price: null,
        category_id: null,
        is_bought: false,
        position: nextPosition(itemsRef.current),
        ...input,
      };
      const local: Item = { ...full, id, list_id: listId, bought_by: null, bought_at: null, version: 0, updated_at: new Date().toISOString() };
      setItems((cur) => [...cur, local]);
      try {
        accept(await itemsApi.put(listId, id, full));
      } catch (e) {
        fail(e);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [listId],
  );

  const update = useCallback(
    async (id: string, patch: ItemPatch) => {
      setItems((cur) => cur.map((i) => (i.id === id ? { ...i, ...patch } : i)));
      try {
        accept(await itemsApi.patch(listId, id, patch));
      } catch (e) {
        fail(e);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [listId],
  );

  const remove = useCallback(
    async (id: string) => {
      setItems((cur) => cur.filter((i) => i.id !== id));
      try {
        await itemsApi.remove(listId, id);
      } catch (e) {
        fail(e);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [listId],
  );

  const clearBought = useCallback(async () => {
    setItems((cur) => cur.filter((i) => !i.is_bought));
    try {
      await itemsApi.clearBought(listId);
    } catch (e) {
      fail(e);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listId]);

  return { items: sortItems(items), error, refreshing, refresh, add, update, remove, clearBought };
}

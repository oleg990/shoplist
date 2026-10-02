import { useCallback, useEffect, useSyncExternalStore } from 'react';
import type { ItemInput, ItemPatch } from '../api/items';
import { useSyncEngine } from '../sync/SyncProvider';

// Позиции списка из локальной базы. Правки применяются сразу и уходят на сервер,
// когда есть связь (см. src/sync/engine.ts).
export function useListItems(listId: string) {
  const engine = useSyncEngine();
  const items = useSyncExternalStore(engine.subscribe, useCallback(() => engine.getItems(listId), [engine, listId]));
  const status = useSyncExternalStore(engine.subscribe, engine.getStatus);

  useEffect(() => {
    void engine.sync();
  }, [engine, listId]);

  return {
    items,
    status,
    refresh: () => engine.sync(),
    add: (input: Partial<ItemInput> & { name: string }) => engine.addItem(listId, input),
    update: (id: string, patch: ItemPatch) => engine.updateItem(listId, id, patch),
    remove: (id: string) => engine.removeItem(listId, id),
    clearBought: () => engine.clearBought(listId),
  };
}

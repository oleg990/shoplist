import { useSyncExternalStore } from 'react';
import { useSyncEngine } from './SyncProvider';

export function useLists() {
  const e = useSyncEngine();
  return useSyncExternalStore(e.subscribe, e.getLists);
}

export function useSyncStatus() {
  const e = useSyncEngine();
  return useSyncExternalStore(e.subscribe, e.getStatus);
}

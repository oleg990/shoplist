import * as Crypto from 'expo-crypto';
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { AppState } from 'react-native';
import { API_URL, api } from '../api';
import { itemsApi } from '../api/items';
import { listsApi } from '../api/lists';
import { useAuth } from '../auth/AuthContext';
import { SyncEngine, type Remote } from './engine';
import { claimLocalData } from './localData';
import { RealtimeClient, wsUrl } from './realtime';

const remote: Remote = {
  lists: listsApi.all,
  changes: itemsApi.changes,
  put: itemsApi.put,
  patch: itemsApi.patch,
  remove: itemsApi.remove,
  clearBought: itemsApi.clearBought,
};

const Ctx = createContext<SyncEngine | null>(null);

export function useSyncEngine(): SyncEngine {
  const e = useContext(Ctx);
  if (!e) throw new Error('useSyncEngine must be used inside SyncProvider');
  return e;
}

// Поднимает локальную базу, синхронизацию и WebSocket для вошедшего пользователя.
export function SyncProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const userId = user?.id;
  const [engine, setEngine] = useState<SyncEngine | null>(null);

  useEffect(() => {
    if (!userId) return;
    let alive = true;
    let cleanup = () => {};
    (async () => {
      const kv = await claimLocalData(userId);
      const e = new SyncEngine(kv, remote, () => Crypto.randomUUID());
      await e.load();
      if (!alive) return e.dispose();

      const rt = new RealtimeClient({
        url: wsUrl(API_URL),
        getToken: async () => (await api.session())?.access_token ?? null,
        refreshToken: async () => void (await api.me()),
        onEvent: (ev) => {
          if (ev.type === 'list_changed' && ev.kind === 'items') void e.onListChanged(ev.list_id);
          else void e.sync(); // ready, resync, изменения списков и участников
        },
      });
      rt.start();

      const appState = AppState.addEventListener('change', (s) => s === 'active' && void e.sync());
      // Подстраховка: если связи не было или есть неотправленное, пробуем снова.
      const tick = setInterval(() => {
        const st = e.getStatus();
        if (!st.online || st.pending > 0) void e.sync();
      }, 30000);

      setEngine(e);
      void e.sync();
      cleanup = () => {
        clearInterval(tick);
        appState.remove();
        rt.stop();
        e.dispose();
      };
    })();
    return () => {
      alive = false;
      cleanup();
      setEngine(null);
    };
  }, [userId]);

  if (!engine) return null;
  return <Ctx.Provider value={engine}>{children}</Ctx.Provider>;
}

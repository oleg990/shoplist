import { Platform } from 'react-native';
import { MemoryKv, type KeyValueStore } from './kv';

let kvPromise: Promise<KeyValueStore> | null = null;

// Одно хранилище на приложение. В вебе (только для разработки) — в памяти.
export function getKv(): Promise<KeyValueStore> {
  if (!kvPromise) {
    kvPromise =
      Platform.OS === 'web'
        ? Promise.resolve(new MemoryKv())
        : import('./sqliteKv').then((m) => m.openSqliteKv());
  }
  return kvPromise;
}

// Данные на устройстве принадлежат одному пользователю; чужие стираются при входе.
export async function claimLocalData(userId: string): Promise<KeyValueStore> {
  const kv = await getKv();
  if ((await kv.get('owner')) !== userId) {
    await kv.clear();
    await kv.set('owner', userId);
  }
  return kv;
}

export async function clearLocalData() {
  try {
    await (await getKv()).clear();
  } catch {}
}

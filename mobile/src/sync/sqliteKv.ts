import * as SQLite from 'expo-sqlite';
import type { KeyValueStore } from './kv';

export async function openSqliteKv(name = 'shoplist.db'): Promise<KeyValueStore> {
  const db = await SQLite.openDatabaseAsync(name);
  await db.execAsync('CREATE TABLE IF NOT EXISTS kv (key TEXT PRIMARY KEY NOT NULL, value TEXT NOT NULL)');
  return {
    async get(key) {
      const row = await db.getFirstAsync<{ value: string }>('SELECT value FROM kv WHERE key = ?', key);
      return row?.value ?? null;
    },
    async set(key, value) {
      await db.runAsync('INSERT OR REPLACE INTO kv (key, value) VALUES (?, ?)', key, value);
    },
    async remove(key) {
      await db.runAsync('DELETE FROM kv WHERE key = ?', key);
    },
    async clear() {
      await db.runAsync('DELETE FROM kv');
    },
  };
}

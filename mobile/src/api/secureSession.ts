import * as SecureStore from 'expo-secure-store';
import type { SessionStore, Tokens } from './client';

const KEY = 'shoplist.session';

export const secureSessionStore: SessionStore = {
  async load() {
    try {
      const raw = await SecureStore.getItemAsync(KEY);
      return raw ? (JSON.parse(raw) as Tokens) : null;
    } catch {
      return null;
    }
  },
  async save(tokens) {
    await SecureStore.setItemAsync(KEY, JSON.stringify(tokens));
  },
  async clear() {
    await SecureStore.deleteItemAsync(KEY);
  },
};

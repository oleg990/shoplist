import { Platform } from 'react-native';
import * as SecureStore from 'expo-secure-store';
import type { SessionStore, Tokens } from './client';

const KEY = 'shoplist.session';

// В вебе SecureStore недоступен: сессия лежит в localStorage (только для разработки и просмотра).
const webStore: SessionStore = {
  async load() {
    try {
      const raw = globalThis.localStorage?.getItem(KEY);
      return raw ? (JSON.parse(raw) as Tokens) : null;
    } catch {
      return null;
    }
  },
  async save(tokens) {
    globalThis.localStorage?.setItem(KEY, JSON.stringify(tokens));
  },
  async clear() {
    globalThis.localStorage?.removeItem(KEY);
  },
};

const nativeStore: SessionStore = {
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

export const secureSessionStore: SessionStore = Platform.OS === 'web' ? webStore : nativeStore;

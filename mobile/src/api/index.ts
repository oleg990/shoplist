import { ApiClient } from './client';
import { secureSessionStore } from './secureSession';

// Адрес сервера задаётся при сборке: EXPO_PUBLIC_API_URL (по умолчанию — локальный сервер).
export const API_URL = process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8080';

let onSignedOut: (() => void) | undefined;
export const setSignedOutHandler = (h: (() => void) | undefined) => {
  onSignedOut = h;
};

export const api = new ApiClient({
  baseUrl: API_URL,
  store: secureSessionStore,
  onSignedOut: () => onSignedOut?.(),
});

export { ApiError } from './client';
export type { Tokens, User } from './client';

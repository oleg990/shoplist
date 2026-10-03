import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api, setSignedOutHandler, type Tokens, type User } from '../api';
import { unregisterPush } from '../push/register';
import { clearLocalData } from '../sync/localData';

type AuthState = {
  loading: boolean;
  user: User | null;
  signIn: (username: string, password: string) => Promise<void>;
  recover: (username: string, code: string, newPassword: string) => Promise<void>;
  // Создаёт аккаунт, но не входит: экран показывает коды восстановления и затем зовёт finishRegistration.
  register: (username: string, password: string, name: string) => Promise<Tokens>;
  finishRegistration: (tokens: Tokens) => Promise<void>;
  signOut: () => Promise<void>;
  deleteAccount: () => Promise<void>;
};

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    setSignedOutHandler(() => {
      setUser(null);
      void clearLocalData();
    });
    let alive = true;
    (async () => {
      const s = await api.session();
      // Сохранённая сессия: пользователь известен сразу, без сети (работа офлайн).
      if (alive && s) setUser(s.user);
      if (alive) setLoading(false);
    })();
    return () => {
      alive = false;
      setSignedOutHandler(undefined);
    };
  }, []);

  const signIn = useCallback(async (username: string, password: string) => {
    setUser(await api.login(username, password));
  }, []);

  const recover = useCallback(async (username: string, code: string, newPassword: string) => {
    setUser(await api.recover(username, code, newPassword));
  }, []);

  const register = useCallback((username: string, password: string, name: string) => api.register(username, password, name), []);

  const finishRegistration = useCallback(async (tokens: Tokens) => {
    await api.setSession(tokens);
    setUser(tokens.user);
  }, []);

  const signOut = useCallback(async () => {
    await unregisterPush();
    await api.logout();
    setUser(null);
    await clearLocalData();
  }, []);

  // Удаляет аккаунт на сервере (списки, где есть другие участники, переходят им), затем выходит.
  const deleteAccount = useCallback(async () => {
    await unregisterPush();
    await api.request('DELETE', '/api/v1/me');
    await api.clearSession();
    setUser(null);
    await clearLocalData();
  }, []);

  const value = useMemo(
    () => ({ loading, user, signIn, recover, register, finishRegistration, signOut, deleteAccount }),
    [loading, user, signIn, recover, register, finishRegistration, signOut, deleteAccount],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAuth must be used inside AuthProvider');
  return v;
}

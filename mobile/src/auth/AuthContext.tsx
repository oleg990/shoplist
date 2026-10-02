import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api, setSignedOutHandler, type User } from '../api';
import { unregisterPush } from '../push/register';
import { clearLocalData } from '../sync/localData';

type AuthState = {
  loading: boolean;
  user: User | null;
  signIn: (email: string, code: string) => Promise<void>;
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

  const signIn = useCallback(async (email: string, code: string) => {
    setUser(await api.verifyCode(email, code));
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

  const value = useMemo(() => ({ loading, user, signIn, signOut, deleteAccount }), [loading, user, signIn, signOut, deleteAccount]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAuth must be used inside AuthProvider');
  return v;
}

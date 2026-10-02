import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api, setSignedOutHandler, type User } from '../api';

type AuthState = {
  loading: boolean;
  user: User | null;
  signIn: (email: string, code: string) => Promise<void>;
  signOut: () => Promise<void>;
};

const Ctx = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<User | null>(null);

  useEffect(() => {
    setSignedOutHandler(() => setUser(null));
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
    await api.logout();
    setUser(null);
  }, []);

  const value = useMemo(() => ({ loading, user, signIn, signOut }), [loading, user, signIn, signOut]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useAuth must be used inside AuthProvider');
  return v;
}

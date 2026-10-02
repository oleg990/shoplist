// HTTP-клиент: подставляет access-токен, при 401 один раз обновляет его по refresh-токену.

export type User = { id: string; email: string; name: string };

export type Tokens = {
  access_token: string;
  refresh_token: string;
  expires_in: number;
  user: User;
};

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public retryAfter?: number,
  ) {
    super(message);
  }
}

// Хранилище сессии (на устройстве — SecureStore, в тестах — память).
export interface SessionStore {
  load(): Promise<Tokens | null>;
  save(tokens: Tokens): Promise<void>;
  clear(): Promise<void>;
}

export type ClientOptions = {
  baseUrl: string;
  store: SessionStore;
  fetchFn?: typeof fetch;
  // Вызывается, когда сессия окончательно недействительна (refresh отклонён).
  onSignedOut?: () => void;
};

export class ApiClient {
  private tokens: Tokens | null = null;
  private loaded = false;
  private refreshing: Promise<boolean> | null = null;
  private fetchFn: typeof fetch;

  constructor(private opts: ClientOptions) {
    this.fetchFn = opts.fetchFn ?? ((...a) => fetch(...a));
  }

  async session(): Promise<Tokens | null> {
    if (!this.loaded) {
      this.tokens = await this.opts.store.load();
      this.loaded = true;
    }
    return this.tokens;
  }

  async setSession(t: Tokens) {
    this.tokens = t;
    this.loaded = true;
    await this.opts.store.save(t);
  }

  async clearSession() {
    this.tokens = null;
    this.loaded = true;
    await this.opts.store.clear();
  }

  private async raw(method: string, path: string, body?: unknown, auth = false): Promise<Response> {
    const headers: Record<string, string> = {};
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    if (auth) {
      const s = await this.session();
      if (s) headers.Authorization = `Bearer ${s.access_token}`;
    }
    let res: Response;
    try {
      res = await this.fetchFn(this.opts.baseUrl + path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      throw new ApiError(0, 'Нет связи с сервером');
    }
    return res;
  }

  private async toError(res: Response): Promise<ApiError> {
    let msg = `HTTP ${res.status}`;
    try {
      const j = await res.json();
      if (j && typeof j.error === 'string') msg = j.error;
    } catch {}
    const ra = Number(res.headers.get('Retry-After'));
    return new ApiError(res.status, msg, Number.isFinite(ra) && ra > 0 ? ra : undefined);
  }

  // Параллельные 401 делят один запрос refresh: refresh-токены одноразовые.
  private refreshTokens(): Promise<boolean> {
    if (!this.refreshing) {
      this.refreshing = this.doRefresh().finally(() => {
        this.refreshing = null;
      });
    }
    return this.refreshing;
  }

  private async doRefresh(): Promise<boolean> {
    const s = await this.session();
    if (!s) return false;
    const res = await this.raw('POST', '/api/v1/auth/refresh', { refresh_token: s.refresh_token });
    if (res.ok) {
      await this.setSession((await res.json()) as Tokens);
      return true;
    }
    if (res.status === 401) {
      await this.clearSession();
      this.opts.onSignedOut?.();
    }
    // Остальные ошибки (5xx) — временные, сессию не трогаем.
    return false;
  }

  async request<T>(method: string, path: string, body?: unknown, auth = true): Promise<T> {
    let res = await this.raw(method, path, body, auth);
    if (res.status === 401 && auth && (await this.session())) {
      if (await this.refreshTokens()) res = await this.raw(method, path, body, true);
    }
    if (!res.ok) throw await this.toError(res);
    if (res.status === 204) return undefined as T;
    return (await res.json()) as T;
  }

  requestCode(email: string) {
    return this.request<void>('POST', '/api/v1/auth/request-code', { email }, false);
  }

  async verifyCode(email: string, code: string): Promise<User> {
    const t = await this.request<Tokens>('POST', '/api/v1/auth/verify', { email, code }, false);
    await this.setSession(t);
    return t.user;
  }

  me() {
    return this.request<User>('GET', '/api/v1/me');
  }

  async logout() {
    const s = await this.session();
    if (s) {
      // Сбой сети не должен мешать выйти на устройстве.
      await this.request<void>('POST', '/api/v1/auth/logout', { refresh_token: s.refresh_token }, false).catch(() => {});
    }
    await this.clearSession();
  }
}

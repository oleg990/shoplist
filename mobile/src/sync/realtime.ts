// WebSocket-канал «в списке что-то изменилось» (протокол: docs/realtime.md).
// Данные по нему не приходят: на событие клиент запрашивает изменения обычным запросом.

export type RealtimeEvent =
  | { type: 'ready' }
  | { type: 'resync' }
  | { type: 'list_changed'; list_id: string; kind: 'items' | 'members' | 'list'; version?: number };

type SocketLike = {
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  send(data: string): void;
  close(): void;
};

export type RealtimeOptions = {
  url: string;
  getToken: () => Promise<string | null>;
  // Обновить access-токен (после кода 4401).
  refreshToken: () => Promise<void>;
  onEvent: (e: RealtimeEvent) => void;
  // true после «ready», false при обрыве.
  onConnection?: (connected: boolean) => void;
  createSocket?: (url: string) => SocketLike;
  minDelayMs?: number;
  maxDelayMs?: number;
};

export class RealtimeClient {
  private socket: SocketLike | null = null;
  private stopped = true;
  private attempt = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;

  constructor(private o: RealtimeOptions) {}

  start() {
    if (!this.stopped) return;
    this.stopped = false;
    void this.connect();
  }

  stop() {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    const s = this.socket;
    this.socket = null;
    if (s) {
      s.onclose = null;
      s.close();
    }
  }

  private async connect() {
    const token = await this.o.getToken();
    if (this.stopped) return;
    if (!token) return this.retry();
    const sock = (this.o.createSocket ?? ((u) => new WebSocket(u) as unknown as SocketLike))(this.o.url);
    this.socket = sock;
    sock.onopen = () => sock.send(JSON.stringify({ type: 'auth', token }));
    sock.onmessage = (ev) => {
      let msg: RealtimeEvent;
      try {
        msg = JSON.parse(String(ev.data));
      } catch {
        return;
      }
      if (msg.type === 'ready') {
        this.attempt = 0;
        this.o.onConnection?.(true);
      }
      this.o.onEvent(msg);
    };
    sock.onerror = () => {};
    sock.onclose = (ev) => {
      if (this.socket === sock) this.socket = null;
      this.o.onConnection?.(false);
      if (this.stopped) return;
      if (ev.code === 4401) {
        // Токен истёк: обновляем и подключаемся сразу.
        this.o
          .refreshToken()
          .catch(() => {})
          .then(() => !this.stopped && this.retry(true));
      } else {
        this.retry();
      }
    };
  }

  private retry(immediate = false) {
    if (this.stopped) return;
    const min = this.o.minDelayMs ?? 1000;
    const max = this.o.maxDelayMs ?? 30000;
    const delay = immediate ? 0 : Math.min(max, min * 2 ** this.attempt++);
    this.timer = setTimeout(() => {
      this.timer = null;
      void this.connect();
    }, delay);
  }
}

export function wsUrl(apiUrl: string): string {
  return apiUrl.replace(/^http/, 'ws').replace(/\/+$/, '') + '/api/v1/ws';
}

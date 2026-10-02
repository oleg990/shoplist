import { ApiError } from './api/client';

// Понятный пользователю текст ошибки (интерфейс только на русском).
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return 'Нет связи с сервером. Проверьте интернет.';
    if (e.status === 400) return 'Проверьте введённые данные.';
    if (e.status === 401) return 'Неверный или просроченный код.';
    if (e.status === 429) return `Код уже отправлен. Повторите через ${e.retryAfter ?? 60} с.`;
  }
  return 'Что-то пошло не так. Попробуйте ещё раз.';
}

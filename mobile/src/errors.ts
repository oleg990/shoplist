import { ApiError } from './api/client';

// Понятный пользователю текст ошибки (интерфейс только на русском).
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return 'Нет связи с сервером. Проверьте интернет.';
    if (e.status === 400) return 'Проверьте введённые данные.';
    if (e.status === 401) return 'Неверный или просроченный код.';
    if (e.status === 403) return 'Это может сделать только владелец списка.';
    if (e.status === 404) return 'Не найдено. Возможно, код приглашения неверный или устарел.';
    if (e.status === 409) return 'Действие недоступно: список заполнен или вы его владелец.';
    if (e.status === 429) return `Код уже отправлен. Повторите через ${e.retryAfter ?? 60} с.`;
  }
  return 'Что-то пошло не так. Попробуйте ещё раз.';
}

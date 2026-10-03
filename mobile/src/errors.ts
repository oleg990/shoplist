import { ApiError } from './api/client';

// Понятный пользователю текст ошибки (интерфейс только на русском).
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return 'Нет связи с сервером. Проверьте интернет.';
    if (e.status === 400) {
      if (e.message.startsWith('username')) return 'Логин: от 3 до 32 символов, латиница, цифры, «.», «_», «-».';
      if (e.message.startsWith('password')) return 'Пароль: от 8 до 128 символов.';
      return 'Проверьте введённые данные.';
    }
    if (e.status === 401) return 'Неверные данные. Проверьте логин, пароль или код.';
    if (e.status === 403) return 'Это может сделать только владелец списка.';
    if (e.status === 404) return 'Не найдено. Возможно, код приглашения неверный или устарел.';
    if (e.status === 409 && e.message === 'username is taken') return 'Этот логин уже занят.';
    if (e.status === 409) return 'Действие недоступно: список заполнен или вы его владелец.';
    if (e.status === 429 && (e.retryAfter ?? 0) > 120) return `Слишком много неудачных попыток. Повторите через ${Math.ceil((e.retryAfter ?? 900) / 60)} мин.`;
    if (e.status === 429) return `Слишком часто. Повторите через ${e.retryAfter ?? 60} с.`;
  }
  return 'Что-то пошло не так. Попробуйте ещё раз.';
}

import Constants from 'expo-constants';
import * as Device from 'expo-device';
import * as Notifications from 'expo-notifications';
import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';
import { api } from '../api';

const TOKEN_KEY = 'shoplist.pushToken';

// Куда вести пользователя по нажатию на уведомление (данные задаёт сервер: docs/push.md).
export function notificationTarget(data: unknown): { listId: string } | null {
  if (!data || typeof data !== 'object') return null;
  const id = (data as { list_id?: unknown }).list_id;
  return typeof id === 'string' && id ? { listId: id } : null;
}

export type PushResult = 'registered' | 'denied' | 'unsupported' | 'no-project' | 'failed';

// Просит разрешение, получает push-токен Expo и сообщает его серверу. Безопасно вызывать при каждом запуске.
export async function registerForPush(): Promise<PushResult> {
  if (Platform.OS !== 'android' && Platform.OS !== 'ios') return 'unsupported';
  // В эмуляторах push-токен получить нельзя.
  if (!Device.isDevice) return 'unsupported';
  try {
    if (Platform.OS === 'android') {
      await Notifications.setNotificationChannelAsync('default', {
        name: 'Списки покупок',
        importance: Notifications.AndroidImportance.DEFAULT,
      });
    }
    let { status } = await Notifications.getPermissionsAsync();
    if (status !== 'granted') status = (await Notifications.requestPermissionsAsync()).status;
    if (status !== 'granted') return 'denied';

    // projectId появляется после `eas init` (см. docs/mobile-release.md).
    const projectId = Constants.expoConfig?.extra?.eas?.projectId ?? Constants.easConfig?.projectId;
    if (!projectId) return 'no-project';

    const { data: token } = await Notifications.getExpoPushTokenAsync({ projectId });
    await api.request('PUT', '/api/v1/devices', { expo_push_token: token, platform: Platform.OS });
    await SecureStore.setItemAsync(TOKEN_KEY, token);
    return 'registered';
  } catch {
    return 'failed';
  }
}

// Перед выходом из аккаунта: сервер перестаёт слать уведомления на это устройство.
export async function unregisterPush(): Promise<void> {
  try {
    const token = await SecureStore.getItemAsync(TOKEN_KEY);
    if (!token) return;
    await api.request('DELETE', '/api/v1/devices', { expo_push_token: token });
    await SecureStore.deleteItemAsync(TOKEN_KEY);
  } catch {
    // Нет сети — не мешаем выйти; сервер сам удалит недействительный токен.
  }
}

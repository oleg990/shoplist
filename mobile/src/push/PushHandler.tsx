import * as Notifications from 'expo-notifications';
import { useRouter } from 'expo-router';
import { useEffect } from 'react';
import { Platform } from 'react-native';
import { notificationTarget, registerForPush } from './register';

// В открытом приложении уведомление показываем баннером.
// В вебе push не поддерживается.
if (Platform.OS !== 'web') {
  Notifications.setNotificationHandler({
    handleNotification: async () => ({ shouldShowBanner: true, shouldShowList: true, shouldPlaySound: true, shouldSetBadge: false }),
  });
}

// Регистрирует устройство и открывает нужный список по нажатию на уведомление.
export function PushHandler() {
  const router = useRouter();

  useEffect(() => {
    if (Platform.OS === 'web') return;
    void registerForPush();

    const open = (data: unknown) => {
      const t = notificationTarget(data);
      if (t) router.push({ pathname: '/list/[id]', params: { id: t.listId } });
    };
    // Приложение запущено нажатием на уведомление.
    const last = Notifications.getLastNotificationResponse();
    if (last) open(last.notification.request.content.data);
    const sub = Notifications.addNotificationResponseReceivedListener((r) => open(r.notification.request.content.data));
    return () => sub.remove();
  }, [router]);

  return null;
}

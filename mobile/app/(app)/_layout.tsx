import { Stack } from 'expo-router';
import { PushHandler } from '../../src/push/PushHandler';
import { SyncProvider } from '../../src/sync/SyncProvider';

export default function AppLayout() {
  return (
    <SyncProvider>
      <PushHandler />
      <Stack>
        <Stack.Screen name="index" options={{ title: 'Мои списки' }} />
        <Stack.Screen name="list/[id]/index" options={{ title: 'Список' }} />
        <Stack.Screen name="list/[id]/members" options={{ title: 'Участники' }} />
        <Stack.Screen name="account" options={{ title: 'Аккаунт' }} />
      </Stack>
    </SyncProvider>
  );
}

import { Stack } from 'expo-router';
import { SyncProvider } from '../../src/sync/SyncProvider';

export default function AppLayout() {
  return (
    <SyncProvider>
      <Stack>
        <Stack.Screen name="index" options={{ title: 'Мои списки' }} />
        <Stack.Screen name="list/[id]/index" options={{ title: 'Список' }} />
        <Stack.Screen name="list/[id]/members" options={{ title: 'Участники' }} />
      </Stack>
    </SyncProvider>
  );
}

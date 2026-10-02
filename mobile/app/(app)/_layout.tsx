import { Stack } from 'expo-router';

export default function AppLayout() {
  return (
    <Stack>
      <Stack.Screen name="index" options={{ title: 'Мои списки' }} />
      <Stack.Screen name="list/[id]/index" options={{ title: 'Список' }} />
      <Stack.Screen name="list/[id]/members" options={{ title: 'Участники' }} />
    </Stack>
  );
}

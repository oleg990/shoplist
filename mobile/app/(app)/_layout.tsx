import { Stack } from 'expo-router';

export default function AppLayout() {
  return (
    <Stack>
      <Stack.Screen name="index" options={{ title: 'Мои списки' }} />
      <Stack.Screen name="list/[id]" options={{ title: 'Список' }} />
    </Stack>
  );
}

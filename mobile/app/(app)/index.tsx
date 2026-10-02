import { StyleSheet, Text } from 'react-native';
import { useAuth } from '../../src/auth/AuthContext';
import { Button } from '../../src/ui/Button';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

// Заглушка: списки покупок появятся в следующем PR.
export default function Home() {
  const { user, signOut } = useAuth();
  return (
    <Screen>
      <Text style={styles.title}>Вы вошли</Text>
      <Text style={styles.hint}>{user?.email}</Text>
      <Button title="Выйти" variant="link" onPress={signOut} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 28, fontWeight: '700', color: colors.text },
  hint: { fontSize: 16, color: colors.muted },
});

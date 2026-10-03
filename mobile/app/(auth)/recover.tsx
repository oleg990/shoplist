import { useRouter } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, Text, TextInput } from 'react-native';
import { useAuth } from '../../src/auth/AuthContext';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

export default function Recover() {
  const router = useRouter();
  const { recover } = useAuth();
  const [username, setUsername] = useState('');
  const [code, setCode] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    setBusy(true);
    setError('');
    try {
      await recover(username.trim(), code.trim().toUpperCase(), password);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  return (
    <Screen>
      <Text style={styles.title}>Сброс пароля</Text>
      <Text style={styles.hint}>Введите логин, один из кодов восстановления, выданных при регистрации, и новый пароль.</Text>
      <TextInput style={styles.input} value={username} onChangeText={setUsername} placeholder="Логин" autoCapitalize="none" autoCorrect={false} />
      <TextInput style={styles.input} value={code} onChangeText={setCode} placeholder="XXXXX-XXXXX" autoCapitalize="characters" autoCorrect={false} />
      <TextInput
        style={styles.input}
        value={password}
        onChangeText={setPassword}
        placeholder="Новый пароль (от 8 символов)"
        secureTextEntry
        autoCapitalize="none"
        autoComplete="new-password"
        onSubmitEditing={submit}
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Button title="Сменить пароль и войти" onPress={submit} loading={busy} disabled={!username.trim() || code.trim().length < 10 || password.length < 8} />
      <Button title="Назад" variant="link" onPress={() => router.back()} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 28, fontWeight: '700', color: colors.text },
  hint: { fontSize: 15, color: colors.muted },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 12, fontSize: 16, color: colors.text },
  error: { color: colors.error },
});

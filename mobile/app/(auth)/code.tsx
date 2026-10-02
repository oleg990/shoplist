import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, Text, TextInput } from 'react-native';
import { api } from '../../src/api';
import { useAuth } from '../../src/auth/AuthContext';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

export default function Code() {
  const { email } = useLocalSearchParams<{ email: string }>();
  const router = useRouter();
  const { signIn } = useAuth();
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [info, setInfo] = useState('');

  const submit = async () => {
    setBusy(true);
    setError('');
    try {
      await signIn(email, code);
      // Переход на главный экран делает Guard в корневом layout.
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  };

  const resend = async () => {
    setError('');
    setInfo('');
    try {
      await api.requestCode(email);
      setInfo('Новый код отправлен.');
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <Screen>
      <Text style={styles.title}>Введите код</Text>
      <Text style={styles.hint}>Мы отправили 6-значный код на {email}.</Text>
      <TextInput
        style={styles.input}
        value={code}
        onChangeText={(v) => setCode(v.replace(/\D/g, '').slice(0, 6))}
        placeholder="000000"
        keyboardType="number-pad"
        autoComplete="one-time-code"
        textContentType="oneTimeCode"
        maxLength={6}
        onSubmitEditing={submit}
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}
      {info ? <Text style={styles.hint}>{info}</Text> : null}
      <Button title="Войти" onPress={submit} loading={busy} disabled={code.length !== 6} />
      <Button title="Отправить код ещё раз" variant="link" onPress={resend} />
      <Button title="Другой email" variant="link" onPress={() => router.back()} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 28, fontWeight: '700', color: colors.text },
  hint: { fontSize: 16, color: colors.muted },
  input: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 8,
    padding: 12,
    fontSize: 24,
    letterSpacing: 6,
    textAlign: 'center',
    color: colors.text,
  },
  error: { color: colors.error },
});

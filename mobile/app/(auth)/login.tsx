import { useRouter } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, Text, TextInput } from 'react-native';
import { api } from '../../src/api';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

export default function Login() {
  const router = useRouter();
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const submit = async () => {
    const value = email.trim().toLowerCase();
    setBusy(true);
    setError('');
    try {
      await api.requestCode(value);
      router.push({ pathname: '/code', params: { email: value } });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <Text style={styles.title}>ShopList</Text>
      <Text style={styles.hint}>Введите email — мы отправим код для входа.</Text>
      <TextInput
        style={styles.input}
        value={email}
        onChangeText={setEmail}
        placeholder="name@example.com"
        autoCapitalize="none"
        autoComplete="email"
        autoCorrect={false}
        keyboardType="email-address"
        textContentType="emailAddress"
        onSubmitEditing={submit}
      />
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Button title="Получить код" onPress={submit} loading={busy} disabled={!email.includes('@')} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 32, fontWeight: '700', color: colors.text },
  hint: { fontSize: 16, color: colors.muted },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 12, fontSize: 16, color: colors.text },
  error: { color: colors.error },
});

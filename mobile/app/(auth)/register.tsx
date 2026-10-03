import { useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { type Tokens } from '../../src/api';
import { useAuth } from '../../src/auth/AuthContext';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { RecoveryCodes } from '../../src/ui/RecoveryCodes';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

export default function Register() {
  const { register, finishRegistration } = useAuth();
  const [username, setUsername] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [created, setCreated] = useState<Tokens | null>(null);

  const submit = async () => {
    if (password !== repeat) {
      setError('Пароли не совпадают.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      setCreated(await register(username.trim(), password, name.trim()));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  if (created) {
    return (
      <Screen>
        <Text style={styles.title}>Сохраните коды</Text>
        <Text style={styles.hint}>
          Почты у аккаунта нет, поэтому пароль можно сбросить только по такому коду. Каждый код работает один раз. Запишите их или сделайте снимок
          экрана: больше они не покажутся.
        </Text>
        <RecoveryCodes codes={created.recovery_codes ?? []} />
        <Button title="Я сохранил(а) коды" onPress={() => void finishRegistration(created)} />
      </Screen>
    );
  }

  return (
    <Screen>
      <Text style={styles.title}>Новый аккаунт</Text>
      <View style={styles.form}>
        <TextInput
          style={styles.input}
          value={username}
          onChangeText={setUsername}
          placeholder="Логин (латиница, цифры, . _ -)"
          autoCapitalize="none"
          autoComplete="username-new"
          autoCorrect={false}
          textContentType="newPassword"
        />
        <TextInput style={styles.input} value={name} onChangeText={setName} placeholder="Имя (необязательно)" maxLength={50} />
        <TextInput
          style={styles.input}
          value={password}
          onChangeText={setPassword}
          placeholder="Пароль (от 8 символов)"
          secureTextEntry
          autoCapitalize="none"
          autoComplete="new-password"
          textContentType="newPassword"
        />
        <TextInput
          style={styles.input}
          value={repeat}
          onChangeText={setRepeat}
          placeholder="Пароль ещё раз"
          secureTextEntry
          autoCapitalize="none"
          onSubmitEditing={submit}
        />
      </View>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      <Button title="Создать аккаунт" onPress={submit} loading={busy} disabled={username.trim().length < 3 || password.length < 8 || !repeat} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 28, fontWeight: '700', color: colors.text },
  hint: { fontSize: 15, color: colors.muted },
  form: { gap: 10 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 12, fontSize: 16, color: colors.text },
  error: { color: colors.error },
});

import { useEffect, useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { api } from '../../src/api';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { RecoveryCodes } from '../../src/ui/RecoveryCodes';
import { Screen } from '../../src/ui/Screen';
import { colors } from '../../src/ui/theme';

export default function Account() {
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [codesPassword, setCodesPassword] = useState('');
  const [unused, setUnused] = useState<number | null>(null);
  const [codes, setCodes] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [info, setInfo] = useState('');

  useEffect(() => {
    api.unusedRecoveryCodes().then(setUnused, () => {});
  }, []);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    setInfo('');
    try {
      await fn();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const change = () =>
    run(async () => {
      await api.changePassword(oldPassword, newPassword);
      setOldPassword('');
      setNewPassword('');
      setInfo('Пароль изменён. На других устройствах нужно войти заново.');
    });

  const regenerate = () =>
    run(async () => {
      setCodes(await api.newRecoveryCodes(codesPassword));
      setUnused(8);
      setCodesPassword('');
    });

  return (
    <Screen>
      <Text style={styles.title}>Сменить пароль</Text>
      <View style={styles.form}>
        <TextInput style={styles.input} value={oldPassword} onChangeText={setOldPassword} placeholder="Текущий пароль" secureTextEntry autoCapitalize="none" />
        <TextInput style={styles.input} value={newPassword} onChangeText={setNewPassword} placeholder="Новый пароль (от 8 символов)" secureTextEntry autoCapitalize="none" />
        <Button title="Сменить пароль" onPress={change} loading={busy} disabled={!oldPassword || newPassword.length < 8} />
      </View>

      <Text style={styles.title}>Коды восстановления</Text>
      <Text style={styles.hint}>
        {codes ? 'Новые коды. Старые больше не работают. Запишите их: повторно они не покажутся.' : `Неиспользованных кодов: ${unused ?? '…'}. Новые коды заменят старые.`}
      </Text>
      {codes ? <RecoveryCodes codes={codes} /> : null}
      <View style={styles.form}>
        <TextInput style={styles.input} value={codesPassword} onChangeText={setCodesPassword} placeholder="Пароль для подтверждения" secureTextEntry autoCapitalize="none" />
        <Button title="Выпустить новые коды" onPress={regenerate} loading={busy} disabled={!codesPassword} />
      </View>
      {error ? <Text style={styles.error}>{error}</Text> : null}
      {info ? <Text style={styles.hint}>{info}</Text> : null}
    </Screen>
  );
}

const styles = StyleSheet.create({
  title: { fontSize: 22, fontWeight: '700', color: colors.text, marginTop: 8 },
  hint: { fontSize: 15, color: colors.muted },
  form: { gap: 10 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 12, fontSize: 16, color: colors.text },
  error: { color: colors.error },
});

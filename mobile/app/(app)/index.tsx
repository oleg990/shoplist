import { useFocusEffect, useRouter } from 'expo-router';
import { useCallback, useState } from 'react';
import { FlatList, Pressable, RefreshControl, StyleSheet, Text, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { listsApi, type ShopList } from '../../src/api/lists';
import { useAuth } from '../../src/auth/AuthContext';
import { errorMessage } from '../../src/errors';
import { Button } from '../../src/ui/Button';
import { colors } from '../../src/ui/theme';

function members(n: number) {
  const m10 = n % 10;
  const m100 = n % 100;
  const word = m10 === 1 && m100 !== 11 ? 'участник' : m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? 'участника' : 'участников';
  return `${n} ${word}`;
}

export default function Lists() {
  const router = useRouter();
  const { user, signOut } = useAuth();
  const [lists, setLists] = useState<ShopList[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [title, setTitle] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      setLists(await listsApi.all());
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useFocusEffect(
    useCallback(() => {
      load();
    }, [load]),
  );

  const create = async () => {
    setBusy(true);
    try {
      const l = await listsApi.create(title.trim());
      setTitle('');
      router.push({ pathname: '/list/[id]', params: { id: l.id } });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const join = async () => {
    setBusy(true);
    try {
      const l = await listsApi.accept(code);
      setCode('');
      router.push({ pathname: '/list/[id]', params: { id: l.id } });
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <SafeAreaView style={styles.safe} edges={['bottom']}>
      <FlatList
        data={lists}
        keyExtractor={(l) => l.id}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={load} />}
        contentContainerStyle={styles.content}
        ListHeaderComponent={
          <View style={styles.form}>
            {error ? <Text style={styles.error}>{error}</Text> : null}
            <View style={styles.row}>
              <TextInput style={[styles.input, styles.flex]} value={title} onChangeText={setTitle} placeholder="Название нового списка" maxLength={100} onSubmitEditing={create} />
              <Button title="Создать" onPress={create} disabled={!title.trim()} loading={busy} />
            </View>
            <View style={styles.row}>
              <TextInput style={[styles.input, styles.flex]} value={code} onChangeText={setCode} placeholder="Код приглашения" autoCapitalize="characters" autoCorrect={false} onSubmitEditing={join} />
              <Button title="Войти" onPress={join} disabled={!code.trim()} loading={busy} />
            </View>
          </View>
        }
        ListEmptyComponent={refreshing ? null : <Text style={styles.empty}>Списков пока нет. Создайте первый или введите код приглашения.</Text>}
        renderItem={({ item }) => (
          <Pressable style={styles.card} onPress={() => router.push({ pathname: '/list/[id]', params: { id: item.id } })}>
            <Text style={styles.cardTitle}>{item.title}</Text>
            <Text style={styles.muted}>{members(item.member_count)}</Text>
          </Pressable>
        )}
        ListFooterComponent={
          <View style={styles.footer}>
            <Text style={styles.muted}>{user?.email}</Text>
            <Button title="Выйти" variant="link" onPress={signOut} />
          </View>
        }
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bg },
  content: { padding: 16, gap: 10 },
  form: { gap: 10, marginBottom: 8 },
  row: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  flex: { flex: 1 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 10, fontSize: 16, color: colors.text },
  card: { borderWidth: 1, borderColor: colors.border, borderRadius: 10, padding: 14, gap: 4 },
  cardTitle: { fontSize: 18, fontWeight: '600', color: colors.text },
  muted: { color: colors.muted },
  empty: { color: colors.muted, textAlign: 'center', marginTop: 24 },
  error: { color: colors.error },
  footer: { marginTop: 16, alignItems: 'center' },
});

import { Stack, useLocalSearchParams, useRouter } from 'expo-router';
import { useCallback, useEffect, useState } from 'react';
import { ScrollView, Share, StyleSheet, Text, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { listsApi, type Member, type ShopList } from '../../../../src/api/lists';
import { useAuth } from '../../../../src/auth/AuthContext';
import { errorMessage } from '../../../../src/errors';
import { useSyncEngine } from '../../../../src/sync/SyncProvider';
import { confirm } from '../../../../src/ui/confirm';
import { Button } from '../../../../src/ui/Button';
import { colors } from '../../../../src/ui/theme';

export default function ListScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const router = useRouter();
  const { user } = useAuth();
  const engine = useSyncEngine();
  const [list, setList] = useState<ShopList | null>(null);
  const [people, setPeople] = useState<Member[]>([]);
  const [title, setTitle] = useState('');
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const [l, m] = await Promise.all([listsApi.get(id), listsApi.members(id)]);
      setList(l);
      setTitle(l.title);
      setPeople(m);
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [id]);

  useEffect(() => {
    load();
  }, [load]);

  const run = async (fn: () => Promise<void>) => {
    try {
      await fn();
      void engine.sync();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  const isOwner = list?.role === 'owner';

  const rename = () =>
    run(async () => {
      const l = await listsApi.rename(id, title.trim());
      setList(l);
    });

  const invite = () =>
    run(async () => {
      const inv = await listsApi.invite(id);
      await Share.share({ message: `Присоединяйтесь к списку «${list?.title}» в ShopList. Код приглашения: ${inv.code}` });
    });

  const leaveOrDelete = () =>
    isOwner
      ? confirm('Удалить список?', 'Список исчезнет у всех участников.', 'Удалить', () =>
          run(async () => {
            await listsApi.remove(id);
            router.replace('/');
          }),
        )
      : confirm('Покинуть список?', 'Он пропадёт из ваших списков.', 'Покинуть', () =>
          run(async () => {
            await listsApi.removeMember(id, user!.id);
            router.replace('/');
          }),
        );

  const kick = (m: Member) =>
    confirm('Убрать участника?', m.name || 'Участник', 'Убрать', () =>
      run(async () => {
        await listsApi.removeMember(id, m.user_id);
        setPeople((p) => p.filter((x) => x.user_id !== m.user_id));
      }),
    );

  return (
    <SafeAreaView style={styles.safe} edges={['bottom']}>
      <Stack.Screen options={{ title: 'Участники' }} />
      <ScrollView contentContainerStyle={styles.content}>
        {error ? <Text style={styles.error}>{error}</Text> : null}

        {isOwner ? (
          <View style={styles.row}>
            <TextInput style={[styles.input, styles.flex]} value={title} onChangeText={setTitle} maxLength={100} />
            <Button title="Сохранить" onPress={rename} disabled={!title.trim() || title.trim() === list?.title} />
          </View>
        ) : null}

        <Text style={styles.h}>Участники</Text>
        {people.map((m) => (
          <View key={m.user_id} style={styles.member}>
            <Text style={styles.flex}>
              {m.name || 'Без имени'}
              {m.user_id === user?.id ? ' (вы)' : ''}
              {m.role === 'owner' ? ' · владелец' : ''}
            </Text>
            {isOwner && m.user_id !== user?.id ? <Button title="Убрать" variant="link" onPress={() => kick(m)} /> : null}
          </View>
        ))}

        <Button title="Пригласить по коду" onPress={invite} />
        <Button title={isOwner ? 'Удалить список' : 'Покинуть список'} variant="link" onPress={leaveOrDelete} />
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bg },
  content: { padding: 16, gap: 12 },
  row: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  flex: { flex: 1 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 10, fontSize: 16, color: colors.text },
  h: { fontSize: 18, fontWeight: '600', color: colors.text, marginTop: 8 },
  member: { flexDirection: 'row', alignItems: 'center', paddingVertical: 4 },
  muted: { color: colors.muted },
  error: { color: colors.error },
});

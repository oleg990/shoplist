import { Link, Stack, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { FlatList, Modal, Pressable, RefreshControl, ScrollView, StyleSheet, Switch, Text, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { UNITS, unitName, type CatalogItem, type Item } from '../../../../src/api/items';
import { itemSubtitle } from '../../../../src/items/merge';
import { useCatalogSearch } from '../../../../src/items/useCatalogSearch';
import { useListItems } from '../../../../src/items/useListItems';
import { Button } from '../../../../src/ui/Button';
import { colors } from '../../../../src/ui/theme';

// Число из поля ввода: запятая допустима, пустое поле — null, мусор — undefined.
function parseNum(s: string): number | null | undefined {
  const t = s.trim().replace(',', '.');
  if (!t) return null;
  const n = Number(t);
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

export default function ListItems() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { items, status, refresh, add, update, remove, clearBought } = useListItems(id);
  const [text, setText] = useState('');
  const [editing, setEditing] = useState<Item | null>(null);
  const suggestions = useCatalogSearch(text);
  const boughtCount = items.filter((i) => i.is_bought).length;

  const addText = () => {
    const name = text.trim();
    if (!name) return;
    setText('');
    add({ name });
  };

  const addFromCatalog = (c: CatalogItem) => {
    setText('');
    add({ name: c.name, catalog_item_id: c.id, category_id: c.category_id, unit: c.default_unit, quantity: 1 });
  };

  return (
    <SafeAreaView style={styles.safe} edges={['bottom']}>
      <Stack.Screen
        options={{
          title: 'Список',
          headerRight: () => (
            <Link href={{ pathname: '/list/[id]/members', params: { id } }} style={styles.headerLink}>
              Участники
            </Link>
          ),
        }}
      />
      <View style={styles.addBar}>
        <View style={styles.row}>
          <TextInput style={[styles.input, styles.flex]} value={text} onChangeText={setText} placeholder="Что купить?" onSubmitEditing={addText} returnKeyType="done" maxLength={200} />
          <Button title="Добавить" onPress={addText} disabled={!text.trim()} />
        </View>
        {suggestions.length > 0 ? (
          <ScrollView keyboardShouldPersistTaps="handled" style={styles.suggest}>
            {suggestions.map((c) => (
              <Pressable key={c.id} style={styles.suggestRow} onPress={() => addFromCatalog(c)}>
                <Text style={styles.text}>{c.name}</Text>
                {c.category_name ? <Text style={styles.muted}>{c.category_name}</Text> : null}
              </Pressable>
            ))}
          </ScrollView>
        ) : null}
      </View>
      {!status.online ? (
        <Text style={styles.offline}>
          Нет связи{status.pending > 0 ? `, ${status.pending} изм. сохранено на телефоне и уйдёт позже` : ''}
        </Text>
      ) : null}
      {status.error ? <Text style={styles.error}>{status.error}</Text> : null}
      <FlatList
        data={items}
        keyExtractor={(i) => i.id}
        keyboardShouldPersistTaps="handled"
        refreshControl={<RefreshControl refreshing={status.syncing} onRefresh={refresh} />}
        ListEmptyComponent={status.syncing ? null : <Text style={styles.empty}>Список пуст. Добавьте первую позицию.</Text>}
        renderItem={({ item }) => (
          <Pressable style={styles.item} onPress={() => setEditing(item)}>
            <Switch value={item.is_bought} onValueChange={(v) => update(item.id, { is_bought: v })} trackColor={{ true: colors.primary }} />
            <View style={styles.flex}>
              <Text style={[styles.text, item.is_bought && styles.done]}>{item.name}</Text>
              <Text style={styles.muted}>{itemSubtitle(item, unitName(item.unit))}</Text>
            </View>
          </Pressable>
        )}
        ListFooterComponent={
          boughtCount > 0 ? <Button title={`Очистить купленное (${boughtCount})`} variant="link" onPress={clearBought} /> : null
        }
      />
      <EditModal
        item={editing}
        onClose={() => setEditing(null)}
        onSave={(patch) => {
          if (editing) update(editing.id, patch);
          setEditing(null);
        }}
        onDelete={() => {
          if (editing) remove(editing.id);
          setEditing(null);
        }}
      />
    </SafeAreaView>
  );
}

function EditModal({ item, onClose, onSave, onDelete }: { item: Item | null; onClose: () => void; onSave: (p: Partial<Item>) => void; onDelete: () => void }) {
  const [name, setName] = useState('');
  const [qty, setQty] = useState('');
  const [unit, setUnit] = useState<string | null>(null);
  const [price, setPrice] = useState('');
  const [shownId, setShownId] = useState<string | null>(null);

  // Заполняем поля при открытии другой позиции.
  if (item && item.id !== shownId) {
    setShownId(item.id);
    setName(item.name);
    setQty(item.quantity != null ? String(item.quantity) : '');
    setUnit(item.unit);
    setPrice(item.price != null ? String(item.price) : '');
  }
  if (!item && shownId) setShownId(null);

  const q = parseNum(qty);
  const p = parseNum(price);
  const valid = name.trim().length > 0 && q !== undefined && p !== undefined;

  return (
    <Modal visible={!!item} animationType="slide" transparent onRequestClose={onClose}>
      <View style={styles.backdrop}>
        <View style={styles.sheet}>
          <Text style={styles.h}>Позиция</Text>
          <TextInput style={styles.input} value={name} onChangeText={setName} placeholder="Название" maxLength={200} />
          <View style={styles.row}>
            <TextInput style={[styles.input, styles.flex]} value={qty} onChangeText={setQty} placeholder="Количество" keyboardType="decimal-pad" />
            <TextInput style={[styles.input, styles.flex]} value={price} onChangeText={setPrice} placeholder="Цена, ₽" keyboardType="decimal-pad" />
          </View>
          <View style={styles.units}>
            {UNITS.map((u) => (
              <Pressable key={u.code} style={[styles.chip, unit === u.code && styles.chipOn]} onPress={() => setUnit(unit === u.code ? null : u.code)}>
                <Text style={unit === u.code ? styles.chipTextOn : styles.text}>{u.name}</Text>
              </Pressable>
            ))}
          </View>
          <Button title="Сохранить" disabled={!valid} onPress={() => onSave({ name: name.trim(), quantity: q ?? null, unit, price: p ?? null })} />
          <Button title="Удалить позицию" variant="link" onPress={onDelete} />
          <Button title="Отмена" variant="link" onPress={onClose} />
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  safe: { flex: 1, backgroundColor: colors.bg },
  addBar: { padding: 12, gap: 6 },
  row: { flexDirection: 'row', gap: 8, alignItems: 'center' },
  flex: { flex: 1 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 8, padding: 10, fontSize: 16, color: colors.text },
  suggest: { maxHeight: 220, borderWidth: 1, borderColor: colors.border, borderRadius: 8 },
  suggestRow: { padding: 10, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: colors.border },
  item: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 16, paddingVertical: 8 },
  text: { fontSize: 16, color: colors.text },
  done: { textDecorationLine: 'line-through', color: colors.muted },
  muted: { color: colors.muted },
  empty: { color: colors.muted, textAlign: 'center', marginTop: 24 },
  error: { color: colors.error, paddingHorizontal: 16 },
  offline: { color: colors.muted, backgroundColor: '#f3f3f3', paddingHorizontal: 16, paddingVertical: 6 },
  headerLink: { color: colors.primary, fontSize: 16 },
  backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.4)', justifyContent: 'flex-end' },
  sheet: { backgroundColor: colors.bg, padding: 16, gap: 10, borderTopLeftRadius: 16, borderTopRightRadius: 16 },
  h: { fontSize: 18, fontWeight: '600', color: colors.text },
  units: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  chip: { borderWidth: 1, borderColor: colors.border, borderRadius: 16, paddingHorizontal: 12, paddingVertical: 6 },
  chipOn: { backgroundColor: colors.primary, borderColor: colors.primary },
  chipTextOn: { color: '#fff', fontSize: 16 },
});

import type { Item } from '../src/api/items';
import { applyChanges, itemSubtitle, nextPosition, sortItems } from '../src/items/merge';

const item = (id: string, over: Partial<Item> = {}): Item => ({
  id,
  list_id: 'l',
  catalog_item_id: null,
  name: id,
  quantity: null,
  unit: null,
  price: null,
  category_id: null,
  is_bought: false,
  bought_by: null,
  bought_at: null,
  position: 1,
  version: 1,
  updated_at: '',
  ...over,
});

test('applyChanges adds, replaces and removes', () => {
  const cur = [item('a'), item('b')];
  const out = applyChanges(cur, [item('a', { version: 2, is_bought: true }), item('b', { version: 2, deleted: true }), item('c', { version: 3 })]);
  expect(out.map((i) => i.id).sort()).toEqual(['a', 'c']);
  expect(out.find((i) => i.id === 'a')?.is_bought).toBe(true);
});

test('applyChanges keeps a newer local version', () => {
  const out = applyChanges([item('a', { version: 5, name: 'new' })], [item('a', { version: 3, name: 'old' })]);
  expect(out[0].name).toBe('new');
});

test('applyChanges lets the server replace an optimistic item (version 0)', () => {
  const out = applyChanges([item('a', { version: 0, name: 'local' })], [item('a', { version: 1, name: 'server' })]);
  expect(out[0].name).toBe('server');
});

test('sortItems puts bought last, then by position', () => {
  const out = sortItems([item('b', { position: 2 }), item('x', { is_bought: true, position: 1 }), item('a', { position: 1 })]);
  expect(out.map((i) => i.id)).toEqual(['a', 'b', 'x']);
});

test('nextPosition', () => {
  expect(nextPosition([])).toBe(1);
  expect(nextPosition([item('a', { position: 7 })])).toBe(8);
});

test('itemSubtitle', () => {
  expect(itemSubtitle({ quantity: 2, unit: 'kg', price: 150 }, 'кг')).toBe('2 кг · 150 ₽');
  expect(itemSubtitle({ quantity: null, unit: null, price: null }, '')).toBe('');
});

import type { Item } from '../api/items';

// Накладывает изменения сервера (since-лента) на локальный список.
// Позиция с deleted=true убирается; более старая версия не затирает более новую.
export function applyChanges(current: Item[], changes: Item[]): Item[] {
  const byId = new Map(current.map((i) => [i.id, i]));
  for (const c of changes) {
    const old = byId.get(c.id);
    if (old && old.version > c.version) continue;
    if (c.deleted) byId.delete(c.id);
    else byId.set(c.id, c);
  }
  return [...byId.values()];
}

// Сначала не купленное, внутри групп — по позиции, затем по имени.
export function sortItems(items: Item[]): Item[] {
  return [...items].sort(
    (a, b) => Number(a.is_bought) - Number(b.is_bought) || a.position - b.position || a.name.localeCompare(b.name, 'ru'),
  );
}

export function nextPosition(items: Item[]): number {
  return items.reduce((m, i) => Math.max(m, i.position), 0) + 1;
}

// «2 кг · 150 ₽» — краткое описание количества и цены.
export function itemSubtitle(i: Pick<Item, 'quantity' | 'unit' | 'price'>, unitLabel: string): string {
  const parts: string[] = [];
  if (i.quantity != null) parts.push(`${i.quantity}${unitLabel ? ' ' + unitLabel : ''}`);
  if (i.price != null) parts.push(`${i.price} ₽`);
  return parts.join(' · ');
}

import type { ApiClient } from './client';
import { api } from './index';

export const UNITS = [
  { code: 'pcs', name: 'шт' },
  { code: 'kg', name: 'кг' },
  { code: 'g', name: 'г' },
  { code: 'l', name: 'л' },
  { code: 'ml', name: 'мл' },
  { code: 'pack', name: 'уп' },
] as const;

export const unitName = (code: string | null) => UNITS.find((u) => u.code === code)?.name ?? '';

export type Item = {
  id: string;
  list_id: string;
  catalog_item_id: number | null;
  name: string;
  quantity: number | null;
  unit: string | null;
  price: number | null;
  category_id: number | null;
  is_bought: boolean;
  bought_by: string | null;
  bought_at: string | null;
  position: number;
  version: number;
  updated_at: string;
  deleted?: boolean;
};

// Поля, которые клиент задаёт при создании/замене позиции.
export type ItemInput = {
  catalog_item_id: number | null;
  name: string;
  quantity: number | null;
  unit: string | null;
  price: number | null;
  category_id: number | null;
  is_bought: boolean;
  position: number;
};

// Частичное изменение: null очищает поле.
export type ItemPatch = Partial<Pick<ItemInput, 'name' | 'quantity' | 'unit' | 'price' | 'category_id' | 'position' | 'is_bought'>>;

export type CatalogItem = {
  id: number;
  name: string;
  category_id: number | null;
  category_name: string | null;
  default_unit: string | null;
};

export const makeItemsApi = (api: ApiClient) => ({
  changes: (listId: string, since: number) =>
    api.request<{ items: Item[]; cursor: number }>('GET', `/api/v1/lists/${listId}/items?since=${since}`),
  put: (listId: string, id: string, input: ItemInput) => api.request<Item>('PUT', `/api/v1/lists/${listId}/items/${id}`, input),
  patch: (listId: string, id: string, patch: ItemPatch) => api.request<Item>('PATCH', `/api/v1/lists/${listId}/items/${id}`, patch),
  remove: (listId: string, id: string) => api.request<void>('DELETE', `/api/v1/lists/${listId}/items/${id}`),
  clearBought: (listId: string) => api.request<{ cleared: number }>('POST', `/api/v1/lists/${listId}/items/clear-bought`),
});
export const itemsApi = makeItemsApi(api);

export const makeCatalogApi = (api: ApiClient) => ({
  search: (q: string, limit = 8) =>
    api
      .request<{ items: CatalogItem[] }>('GET', `/api/v1/catalog/search?q=${encodeURIComponent(q)}&limit=${limit}`, undefined, false)
      .then((r) => r.items),
});
export const catalogApi = makeCatalogApi(api);

import type { ApiClient } from './client';
import { api } from './index';

export type ShopList = {
  id: string;
  title: string;
  owner_id: string;
  role: 'owner' | 'editor';
  member_count: number;
  created_at: string;
  updated_at: string;
};

export type Member = { user_id: string; name: string; role: 'owner' | 'editor'; joined_at: string };
export type Invite = { code: string; expires_at: string; max_uses: number; uses: number };

export const makeListsApi = (api: ApiClient) => ({
  all: () => api.request<{ items: ShopList[] }>('GET', '/api/v1/lists').then((r) => r.items),
  get: (id: string) => api.request<ShopList>('GET', `/api/v1/lists/${id}`),
  put: (id: string, title: string) => api.request<ShopList>('PUT', `/api/v1/lists/${id}`, { title }),
  create: (title: string) => api.request<ShopList>('POST', '/api/v1/lists', { title }),
  rename: (id: string, title: string) => api.request<ShopList>('PATCH', `/api/v1/lists/${id}`, { title }),
  remove: (id: string) => api.request<void>('DELETE', `/api/v1/lists/${id}`),
  members: (id: string) => api.request<{ items: Member[] }>('GET', `/api/v1/lists/${id}/members`).then((r) => r.items),
  removeMember: (id: string, userId: string) => api.request<void>('DELETE', `/api/v1/lists/${id}/members/${userId}`),
  invite: (id: string) => api.request<Invite>('POST', `/api/v1/lists/${id}/invites`),
  accept: (code: string) => api.request<ShopList>('POST', '/api/v1/invites/accept', { code: code.trim() }),
});
export const listsApi = makeListsApi(api);

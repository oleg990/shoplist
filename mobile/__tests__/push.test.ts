jest.mock('expo-device', () => ({ isDevice: true }));
jest.mock('expo-constants', () => ({ __esModule: true, default: { expoConfig: { extra: { eas: { projectId: 'p1' } } } } }));
jest.mock('expo-secure-store', () => {
  const m = new Map<string, string>();
  return {
    getItemAsync: async (k: string) => m.get(k) ?? null,
    setItemAsync: async (k: string, v: string) => void m.set(k, v),
    deleteItemAsync: async (k: string) => void m.delete(k),
  };
});
const mockNotif = {
  setNotificationChannelAsync: jest.fn(async () => {}),
  AndroidImportance: { DEFAULT: 3 },
  getPermissionsAsync: jest.fn(async () => ({ status: 'undetermined' })),
  requestPermissionsAsync: jest.fn(async () => ({ status: 'granted' })),
  getExpoPushTokenAsync: jest.fn(async () => ({ data: 'ExponentPushToken[abc]' })),
};
jest.mock('expo-notifications', () => ({
  AndroidImportance: { DEFAULT: 3 },
  setNotificationChannelAsync: (...a: unknown[]) => (mockNotif.setNotificationChannelAsync as any)(...a),
  getPermissionsAsync: () => mockNotif.getPermissionsAsync(),
  requestPermissionsAsync: () => mockNotif.requestPermissionsAsync(),
  getExpoPushTokenAsync: (...a: unknown[]) => (mockNotif.getExpoPushTokenAsync as any)(...a),
}));
const mockRequest = jest.fn(async () => undefined);
jest.mock('../src/api', () => ({ api: { request: (...a: unknown[]) => (mockRequest as any)(...a) } }));

import { notificationTarget, registerForPush, unregisterPush } from '../src/push/register';

beforeEach(() => {
  mockRequest.mockClear();
  mockNotif.getExpoPushTokenAsync.mockClear();
  mockNotif.requestPermissionsAsync.mockClear();
  mockNotif.getPermissionsAsync.mockResolvedValue({ status: 'undetermined' });
  mockNotif.requestPermissionsAsync.mockResolvedValue({ status: 'granted' });
});

test('notificationTarget reads list_id from the server payload', () => {
  expect(notificationTarget({ list_id: 'L1', kind: 'items' })).toEqual({ listId: 'L1' });
  expect(notificationTarget({})).toBeNull();
  expect(notificationTarget(null)).toBeNull();
  expect(notificationTarget({ list_id: 5 })).toBeNull();
});

test('registers the token with the server after permission is granted', async () => {
  await expect(registerForPush()).resolves.toBe('registered');
  expect(mockRequest).toHaveBeenCalledWith('PUT', '/api/v1/devices', { expo_push_token: 'ExponentPushToken[abc]', platform: 'ios' });
  expect(mockNotif.getExpoPushTokenAsync).toHaveBeenCalledWith({ projectId: 'p1' });
});

test('does not ask again when permission is already granted', async () => {
  mockNotif.getPermissionsAsync.mockResolvedValue({ status: 'granted' });
  await registerForPush();
  expect(mockNotif.requestPermissionsAsync).not.toHaveBeenCalled();
});

test('denied permission: nothing is sent to the server', async () => {
  mockNotif.requestPermissionsAsync.mockResolvedValue({ status: 'denied' });
  await expect(registerForPush()).resolves.toBe('denied');
  expect(mockRequest).not.toHaveBeenCalled();
});

test('a server failure does not throw', async () => {
  mockRequest.mockRejectedValueOnce(new Error('offline'));
  await expect(registerForPush()).resolves.toBe('failed');
});

test('unregister sends the stored token, once', async () => {
  await registerForPush();
  mockRequest.mockClear();
  await unregisterPush();
  expect(mockRequest).toHaveBeenCalledWith('DELETE', '/api/v1/devices', { expo_push_token: 'ExponentPushToken[abc]' });
  mockRequest.mockClear();
  await unregisterPush();
  expect(mockRequest).not.toHaveBeenCalled();
});

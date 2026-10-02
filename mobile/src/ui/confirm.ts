import { Alert, Platform } from 'react-native';

// Alert.alert на web не показывает кнопки, поэтому там спрашиваем через confirm.
export function confirm(title: string, message: string, action: string, onYes: () => void) {
  if (Platform.OS === 'web') {
    if (globalThis.confirm?.(`${title}\n${message}`)) onYes();
    return;
  }
  Alert.alert(title, message, [
    { text: 'Отмена', style: 'cancel' },
    { text: action, style: 'destructive', onPress: onYes },
  ]);
}

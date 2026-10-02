import { ActivityIndicator, Pressable, StyleSheet, Text } from 'react-native';
import { colors } from './theme';

type Props = { title: string; onPress: () => void; loading?: boolean; disabled?: boolean; variant?: 'primary' | 'link' };

export function Button({ title, onPress, loading, disabled, variant = 'primary' }: Props) {
  const off = disabled || loading;
  const link = variant === 'link';
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      disabled={off}
      style={[link ? styles.link : styles.btn, off && styles.off]}
    >
      {loading ? <ActivityIndicator color={link ? colors.primary : '#fff'} /> : <Text style={link ? styles.linkText : styles.text}>{title}</Text>}
    </Pressable>
  );
}

const styles = StyleSheet.create({
  btn: { backgroundColor: colors.primary, borderRadius: 8, paddingVertical: 14, alignItems: 'center' },
  text: { color: '#fff', fontSize: 16, fontWeight: '600' },
  link: { paddingVertical: 10, alignItems: 'center' },
  linkText: { color: colors.primary, fontSize: 15 },
  off: { opacity: 0.5 },
});

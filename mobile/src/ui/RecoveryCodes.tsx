import { StyleSheet, Text, View } from 'react-native';
import { colors } from './theme';

export function RecoveryCodes({ codes }: { codes: string[] }) {
  return (
    <View style={styles.box}>
      {codes.map((c) => (
        <Text key={c} selectable style={styles.code}>
          {c}
        </Text>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  box: { borderWidth: 1, borderColor: colors.border, borderRadius: 10, padding: 14, gap: 6, alignItems: 'center' },
  code: { fontSize: 20, letterSpacing: 2, color: colors.text, fontVariant: ['tabular-nums'] },
});

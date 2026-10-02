#!/bin/sh
# Раз в сутки (в 03:00 по времени контейнера, UTC) делает pg_dump и удаляет копии старше BACKUP_KEEP_DAYS дней.
# Запуск с аргументом `now` делает одну копию сразу: docker compose run --rm backup now
set -eu

dump() {
	out="/backups/shoplist-$(date -u +%Y%m%d-%H%M%S).sql.gz"
	tmp="$out.part"
	pg_dump --no-owner --clean --if-exists | gzip -9 >"$tmp"
	mv "$tmp" "$out"
	echo "backup written: $out"
	find /backups -name 'shoplist-*.sql.gz' -mtime "+${BACKUP_KEEP_DAYS:-14}" -delete
}

if [ "${1:-}" = "now" ]; then
	dump
	exit 0
fi

while true; do
	now=$(date -u +%s)
	next=$(date -u -d "03:00" +%s 2>/dev/null || echo 0)
	[ "$next" -le "$now" ] && next=$((next + 86400))
	sleep $((next - now))
	dump || echo "backup FAILED" >&2
done

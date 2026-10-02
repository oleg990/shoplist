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
	# Секунд до ближайших 03:00 UTC (арифметика по эпохе: не зависит от формата date в busybox).
	now=$(date -u +%s)
	wait=$(((10800 - now % 86400 + 86400) % 86400))
	[ "$wait" -eq 0 ] && wait=86400
	sleep "$wait"
	dump || echo "backup FAILED" >&2
done

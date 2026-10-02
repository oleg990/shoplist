import { useEffect, useState } from 'react';
import { catalogApi, type CatalogItem } from '../api/items';

// Подсказки из общего каталога с задержкой ввода; ошибки сети молча дают пустой список
// (добавить позицию вручную можно всегда).
export function useCatalogSearch(query: string, delayMs = 250): CatalogItem[] {
  const [found, setFound] = useState<CatalogItem[]>([]);
  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setFound([]);
      return;
    }
    let alive = true;
    const t = setTimeout(() => {
      catalogApi
        .search(q)
        .then((r) => alive && setFound(r))
        .catch(() => alive && setFound([]));
    }, delayMs);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query, delayMs]);
  return found;
}

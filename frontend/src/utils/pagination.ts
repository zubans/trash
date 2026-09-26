// Постраничные списки без общего счётчика (почта, инциденты): сервер отдаёт
// страницу по limit/offset, и следующая есть, пока страница пришла полной.

export interface PageParams {
  limit: number
  offset: number
}

// hasMorePages — пришла ли страница полной. Короткая страница — последняя.
export function hasMorePages(received: number, limit: number): boolean {
  return received >= limit
}

// appendPage дописывает следующую страницу к уже показанной, пропуская то, что
// уже есть. Смещение считается от длины показанного списка, поэтому пришедшая
// за это время свежая запись сдвигает страницы на одну — и первая строка новой
// страницы оказывается той, что уже на экране. Удалённая строка, наоборот, не
// создаёт пропуска: список короче ровно на неё.
export function appendPage<T>(shown: T[], page: T[], keyOf: (item: T) => string): T[] {
  const seen = new Set(shown.map(keyOf))
  return [...shown, ...page.filter((item) => !seen.has(keyOf(item)))]
}

import { useQueries, useQuery } from '@tanstack/react-query';
import { listSpaces, listTags, type Tag } from '@/api/tasks';

export type LabelOption = Pick<Tag, 'name' | 'color'>;

/**
 * Every label (space tag) the person can see, one entry per name. Labels
 * belong to a space, so My Work and the Inbox, which span spaces, filter by
 * name; two spaces with a "Venue" label share one option.
 */
export function useAllLabels(): LabelOption[] {
  const spaces = useQuery({ queryKey: ['spaces'], queryFn: ({ signal }) => listSpaces(signal) });
  const perSpace = useQueries({
    queries: (spaces.data ?? []).map((s) => ({
      queryKey: ['tags', s.id],
      queryFn: ({ signal }: { signal: AbortSignal }) => listTags(s.id, signal),
    })),
  });
  const seen = new Map<string, LabelOption>();
  for (const q of perSpace) {
    for (const tg of q.data ?? []) {
      const key = tg.name.toLowerCase();
      if (!seen.has(key)) seen.set(key, { name: tg.name, color: tg.color });
    }
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name));
}

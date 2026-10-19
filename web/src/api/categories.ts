export const CATEGORIES = [
  { slug: 'food', name: 'еда' },
  { slug: 'transport', name: 'транспорт' },
  { slug: 'housing', name: 'жильё' },
  { slug: 'entertainment', name: 'развлечения' },
  { slug: 'health', name: 'здоровье' },
  { slug: 'shopping', name: 'покупки' },
  { slug: 'other', name: 'другое' },
] as const

export type CategorySlug = (typeof CATEGORIES)[number]['slug']

export const CATEGORY_STYLE: Record<CategorySlug, { color: string; soft: string }> = {
  food: { color: 'var(--cat-food)', soft: 'var(--cat-food-soft)' },
  transport: { color: 'var(--cat-transport)', soft: 'var(--cat-transport-soft)' },
  housing: { color: 'var(--cat-housing)', soft: 'var(--cat-housing-soft)' },
  entertainment: { color: 'var(--cat-fun)', soft: 'var(--cat-fun-soft)' },
  health: { color: 'var(--cat-health)', soft: 'var(--cat-health-soft)' },
  shopping: { color: 'var(--cat-shopping)', soft: 'var(--cat-shopping-soft)' },
  other: { color: 'var(--cat-other)', soft: 'var(--cat-other-soft)' },
}

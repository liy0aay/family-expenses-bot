import type { components } from './schema'

export type User = components['schemas']['User']
export type UserList = components['schemas']['UserList']
export type Expense = components['schemas']['Expense']
export type ExpenseList = components['schemas']['ExpenseList']
export type ExpenseCreate = components['schemas']['ExpenseCreate']
export type ExpensePatch = components['schemas']['ExpensePatch']
export type SummaryItem = components['schemas']['SummaryItem']
export type SummaryResponse = components['schemas']['SummaryResponse']

export type ApiErrorCode =
  | components['schemas']['Error']['error']['code']
  | 'network'

const KNOWN_CODES: ReadonlySet<string> = new Set([
  'invalid_amount',
  'invalid_category',
  'invalid_date',
  'not_found',
  'bad_request',
  'internal',
])

export class ApiError extends Error {
  readonly code: ApiErrorCode
  readonly status: number

  constructor(code: ApiErrorCode, message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

export function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'invalid_amount':
        return 'Сумма: больше нуля и не больше двух знаков после точки'
      case 'invalid_category':
        return 'Выберите категорию из списка'
      case 'invalid_date':
        return 'Дата в формате ГГГГ-ММ-ДД'
      case 'not_found':
        return 'Трата не найдена — возможно, уже удалена'
      case 'bad_request':
        return err.message || 'Проверь данные'
      case 'internal':
        return 'Внутренняя ошибка, попробуй позже'
      case 'network':
        return 'Сервер недоступен'
    }
  }
  return 'Внутренняя ошибка, попробуй позже'
}

const BASE = '/api/v1'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    response = await fetch(BASE + path, {
      ...init,
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    })
  } catch {
    throw new ApiError('network', 'request failed', 0)
  }

  if (response.status === 204) {
    return undefined as T
  }

  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    payload = undefined
  }

  if (!response.ok) {
    const body = payload as { error?: { code?: string; message?: string } } | undefined
    const code = body?.error?.code
    const known = code !== undefined && KNOWN_CODES.has(code)
    throw new ApiError(
      known ? (code as ApiErrorCode) : 'internal',
      body?.error?.message ?? '',
      response.status,
    )
  }

  return payload as T
}

export interface ExpenseFilters {
  userId?: number
  dateFrom?: string
  dateTo?: string
  category?: string
  limit?: number
}

export function listExpenses(filters: ExpenseFilters = {}): Promise<ExpenseList> {
  const query = new URLSearchParams()
  if (filters.userId !== undefined) query.set('user_id', String(filters.userId))
  if (filters.dateFrom) query.set('date_from', filters.dateFrom)
  if (filters.dateTo) query.set('date_to', filters.dateTo)
  if (filters.category) query.set('category', filters.category)
  if (filters.limit !== undefined) query.set('limit', String(filters.limit))
  const qs = query.toString()
  return request(`/expenses${qs ? `?${qs}` : ''}`)
}

export interface SummaryFilters {
  userId?: number
  dateFrom?: string
  dateTo?: string
}

export function getExpensesSummary(filters: SummaryFilters = {}): Promise<SummaryResponse> {
  const query = new URLSearchParams()
  if (filters.userId !== undefined) query.set('user_id', String(filters.userId))
  if (filters.dateFrom) query.set('date_from', filters.dateFrom)
  if (filters.dateTo) query.set('date_to', filters.dateTo)
  const qs = query.toString()
  return request(`/expenses/summary${qs ? `?${qs}` : ''}`)
}

export function listUsers(): Promise<UserList> {
  return request('/users')
}

export function createExpense(body: ExpenseCreate): Promise<Expense> {
  return request('/expenses', { method: 'POST', body: JSON.stringify(body) })
}

export function patchExpense(expenseId: number, body: ExpensePatch): Promise<Expense> {
  return request(`/expenses/${expenseId}`, { method: 'PATCH', body: JSON.stringify(body) })
}

export function deleteExpense(expenseId: number): Promise<void> {
  return request(`/expenses/${expenseId}`, { method: 'DELETE' })
}

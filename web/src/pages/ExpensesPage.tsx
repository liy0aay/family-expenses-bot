import { useCallback, useEffect, useState } from 'react'

import { CATEGORIES, CATEGORY_STYLE } from '../api/categories'
import { errorText, listExpenses, listUsers } from '../api/client'
import type { Expense, User } from '../api/client'
import { ExpenseForm } from '../components/ExpenseForm'
import { CategoryBreakdown } from '../components/CategoryBreakdown'

const LIST_LIMIT = 500

function formatAmount(minor: number): string {
  const rubles = Math.trunc(Math.abs(minor) / 100)
  const kopecks = String(Math.abs(minor) % 100).padStart(2, '0')
  const sign = minor < 0 ? '−' : ''
  return `${sign}${rubles.toLocaleString('ru-RU')},${kopecks} ₽`
}

function monthStartISO(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
}

function formatDate(iso: string): string {
  const [year, month, day] = iso.split('-').map(Number)
  const date = new Date(year, month - 1, day)
  const options: Intl.DateTimeFormatOptions =
    year === new Date().getFullYear()
      ? { day: 'numeric', month: 'long' }
      : { day: 'numeric', month: 'long', year: 'numeric' }
  return date.toLocaleDateString('ru-RU', options)
}

function initials(name: string): string {
  return name.trim().charAt(0).toUpperCase()
}

interface Filters {
  userId: string
  dateFrom: string
  dateTo: string
  category: string
}

const EMPTY_FILTERS: Filters = {
  userId: '',
  dateFrom: monthStartISO(),
  dateTo: '',
  category: '',
}

export function ExpensesPage() {
  const [users, setUsers] = useState<User[]>([])
  const [expenses, setExpenses] = useState<Expense[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS)
  const [editing, setEditing] = useState<Expense | null>(null)
  const [creating, setCreating] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await listExpenses({
        userId: filters.userId ? Number(filters.userId) : undefined,
        dateFrom: filters.dateFrom || undefined,
        dateTo: filters.dateTo || undefined,
        category: filters.category || undefined,
        limit: LIST_LIMIT,
      })
      setExpenses(list.items)
      setError(null)
    } catch (err) {
      setExpenses([])
      setError(errorText(err))
    }
  }, [filters])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    listUsers()
      .then((res) => setUsers(res.items))
      .catch((err) => setError(errorText(err)))
  }, [])

  const setFilter = (key: keyof Filters) => (value: string) =>
    setFilters((prev) => ({ ...prev, [key]: value }))

  const total = (expenses ?? []).reduce((acc, e) => acc + e.amount_minor, 0)

  return (
    <div className="page">
      <section className="card summary">
        <div>
          <div className="summary-title">Расходы за период</div>
          <div className="summary-total">{formatAmount(total)}</div>
          <div className="summary-note">по выбранным фильтрам</div>
        </div>
        <div className="filters">
          <select
            className="control"
            value={filters.userId}
            onChange={(e) => setFilter('userId')(e.target.value)}
            aria-label="Член семьи"
          >
            <option value="">Вся семья</option>
            {users.map((u) => (
              <option key={u.id} value={String(u.id)}>
                {u.first_name}
              </option>
            ))}
          </select>
          <select
            className="control"
            value={filters.category}
            onChange={(e) => setFilter('category')(e.target.value)}
            aria-label="Категория"
          >
            <option value="">Все категории</option>
            {CATEGORIES.map((c) => (
              <option key={c.slug} value={c.slug}>
                {c.name}
              </option>
            ))}
          </select>
          <input
            className="control"
            type="date"
            value={filters.dateFrom}
            onChange={(e) => setFilter('dateFrom')(e.target.value)}
            aria-label="С даты"
          />
          <input
            className="control"
            type="date"
            value={filters.dateTo}
            onChange={(e) => setFilter('dateTo')(e.target.value)}
            aria-label="По дату"
          />
        </div>
      </section>

      {error && <div className="page-error">{error}</div>}

      <section className="card">
        <div className="list-toolbar">
          <button type="button" className="btn btn-primary" onClick={() => setCreating(true)}>
            Добавить трату
          </button>
        </div>

        {expenses === null ? (
          <div className="empty">Загрузка…</div>
        ) : expenses.length === 0 ? (
          <div className="empty">Трат не найдено</div>
        ) : (
          <div className="list">
            {expenses.length === LIST_LIMIT && (
              <div className="hint">Показаны последние {LIST_LIMIT} трат — уточните период</div>
            )}
            {expenses.map((expense) => (
              <ExpenseRow key={expense.id} expense={expense} onEdit={setEditing} />
            ))}
          </div>
        )}
      </section>

      <CategoryBreakdown users={users} dateFrom={filters.dateFrom} dateTo={filters.dateTo} />

      {(creating || editing !== null) && (
        <ExpenseForm
          users={users}
          expense={editing}
          onSaved={() => {
            setCreating(false)
            setEditing(null)
            void load()
          }}
          onClose={() => {
            setCreating(false)
            setEditing(null)
          }}
        />
      )}
    </div>
  )
}

function ExpenseRow({ expense, onEdit }: { expense: Expense; onEdit: (e: Expense) => void }) {
  const style = CATEGORY_STYLE[expense.category]
  return (
    <div className="row">
      <div className="owner">
        <span className="avatar" style={{ background: style.soft, color: style.color }}>
          {initials(expense.user_name)}
        </span>
        <span className="owner-name">{expense.user_name}</span>
      </div>
      <div className="cat">
        <span className="dot" style={{ background: style.color }} />
        <span>{expense.category_name}</span>
      </div>
      <div className="comment">{expense.comment ?? ''}</div>
      <div className="date">{formatDate(expense.spent_at)}</div>
      <div className="amount">{formatAmount(expense.amount_minor)}</div>
      <button type="button" className="row-edit" onClick={() => onEdit(expense)}>
        Изменить
      </button>
    </div>
  )
}

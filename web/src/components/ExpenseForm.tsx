import { useState } from 'react'
import type { FormEvent } from 'react'

import { CATEGORIES } from '../api/categories'
import {
  ApiError,
  createExpense,
  deleteExpense,
  errorText,
  patchExpense,
} from '../api/client'
import type { ApiErrorCode, Expense, ExpensePatch, User } from '../api/client'

interface Props {
  users: User[]
  expense: Expense | null
  onSaved: () => void
  onClose: () => void
}

type FieldKey = 'amount' | 'category' | 'date' | 'form'

const CODE_FIELD: Partial<Record<ApiErrorCode, Exclude<FieldKey, 'form'>>> = {
  invalid_amount: 'amount',
  invalid_category: 'category',
  invalid_date: 'date',
}

function rubString(minor: number): string {
  const abs = Math.abs(minor)
  return `${Math.trunc(abs / 100)}.${String(abs % 100).padStart(2, '0')}`
}

function todayISO(): string {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

export function ExpenseForm({ users, expense, onSaved, onClose }: Props) {
  const [userId, setUserId] = useState(() =>
    expense ? String(expense.user_id) : users[0] ? String(users[0].id) : '',
  )
  const [amount, setAmount] = useState(() => (expense ? rubString(expense.amount_minor) : ''))
  const [category, setCategory] = useState(() => (expense ? expense.category : ''))
  const [spentAt, setSpentAt] = useState(() => (expense ? expense.spent_at : todayISO()))
  const [comment, setComment] = useState(() => expense?.comment ?? '')
  const [errors, setErrors] = useState<Partial<Record<FieldKey, string>>>({})
  const [saving, setSaving] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const fail = (err: unknown) => {
    if (err instanceof ApiError) {
      const field = CODE_FIELD[err.code]
      if (field) {
        setErrors({ [field]: errorText(err) })
        return
      }
    }
    setErrors({ form: errorText(err) })
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setErrors({})
    setSaving(true)
    try {
      if (expense) {
        const patch: ExpensePatch = {}
        if (amount !== rubString(expense.amount_minor)) patch.amount_rub = amount
        if (category !== '' && category !== expense.category) patch.category = category
        if (spentAt !== expense.spent_at) patch.spent_at = spentAt
        if (comment !== (expense.comment ?? '')) patch.comment = comment
        await patchExpense(expense.id, patch)
      } else {
        const body = {
          user_id: Number(userId),
          amount_rub: amount,
          category,
          spent_at: spentAt,
          ...(comment ? { comment } : {}),
        }
        await createExpense(body)
      }
      onSaved()
    } catch (err) {
      fail(err)
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!expense) return
    setErrors({})
    setSaving(true)
    try {
      await deleteExpense(expense.id)
      onSaved()
    } catch (err) {
      setConfirmDelete(false)
      fail(err)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="modal-overlay"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <form className="card modal" onSubmit={submit}>
        <h2 className="modal-title">{expense ? 'Изменить трату' : 'Новая трата'}</h2>

        {expense ? (
          <div className="field">
            <span className="field-label">Член семьи</span>
            <span className="field-static">{expense.user_name}</span>
          </div>
        ) : (
          <label className="field">
            <span className="field-label">Член семьи</span>
            <select
              className="control"
              required
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
            >
              <option value="" disabled>
                Выберите
              </option>
              {users.map((u) => (
                <option key={u.id} value={String(u.id)}>
                  {u.first_name}
                </option>
              ))}
            </select>
          </label>
        )}

        <label className="field">
          <span className="field-label">Сумма, ₽</span>
          <input
            className="control"
            inputMode="decimal"
            placeholder="350.50"
            required
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
          />
          {errors.amount && <span className="field-error">{errors.amount}</span>}
        </label>

        <label className="field">
          <span className="field-label">Категория</span>
          <select
            className="control"
            required
            value={category}
            onChange={(e) => setCategory(e.target.value)}
          >
            <option value="" disabled>
              Выберите
            </option>
            {CATEGORIES.map((c) => (
              <option key={c.slug} value={c.slug}>
                {c.name}
              </option>
            ))}
          </select>
          {errors.category && <span className="field-error">{errors.category}</span>}
        </label>

        <label className="field">
          <span className="field-label">Дата</span>
          <input
            className="control"
            type="date"
            required
            value={spentAt}
            onChange={(e) => setSpentAt(e.target.value)}
          />
          {errors.date && <span className="field-error">{errors.date}</span>}
        </label>

        <label className="field">
          <span className="field-label">Комментарий</span>
          <input
            className="control"
            maxLength={500}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
          />
        </label>

        {errors.form && <div className="form-error">{errors.form}</div>}

        <div className="modal-actions">
          {expense && confirmDelete ? (
            <>
              <span className="confirm-text">Удалить эту трату?</span>
              <button type="button" className="btn btn-danger-ghost" onClick={() => void remove()}>
                Удалить
              </button>
              <button
                type="button"
                className="btn btn-secondary"
                onClick={() => setConfirmDelete(false)}
              >
                Отмена
              </button>
            </>
          ) : (
            <>
              {expense && (
                <button
                  type="button"
                  className="btn btn-danger-ghost"
                  onClick={() => setConfirmDelete(true)}
                >
                  Удалить
                </button>
              )}
              <span className="spacer" />
              <button type="button" className="btn btn-secondary" onClick={onClose}>
                Отмена
              </button>
              <button type="submit" className="btn btn-primary" disabled={saving}>
                Сохранить
              </button>
            </>
          )}
        </div>
      </form>
    </div>
  )
}

import { useEffect, useState } from 'react'

import { CATEGORY_STYLE, type CategorySlug } from '../api/categories'
import { errorText, getExpensesSummary } from '../api/client'
import type { SummaryItem, User } from '../api/client'

function formatAmount(minor: number): string {
  const rubles = Math.trunc(Math.abs(minor) / 100)
  const kopecks = String(Math.abs(minor) % 100).padStart(2, '0')
  const sign = minor < 0 ? '−' : ''
  return `${sign}${rubles.toLocaleString('ru-RU')},${kopecks} ₽`
}

function formatShare(share: number): string {
  return `${(share * 100).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} %`
}

interface Props {
  users: User[]
  dateFrom: string
  dateTo: string
}

export function CategoryBreakdown({ users, dateFrom, dateTo }: Props) {
  const [userId, setUserId] = useState('')
  const [items, setItems] = useState<SummaryItem[] | null>(null)
  const [grandTotal, setGrandTotal] = useState(0)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let active = true
    getExpensesSummary({
      userId: userId ? Number(userId) : undefined,
      dateFrom: dateFrom || undefined,
      dateTo: dateTo || undefined,
    })
      .then((res) => {
        if (!active) return
        setItems(res.items)
        setGrandTotal(res.grand_total_minor)
        setError(null)
      })
      .catch((err) => {
        if (!active) return
        setItems([])
        setError(errorText(err))
      })
    return () => {
      active = false
    }
  }, [userId, dateFrom, dateTo])

  const sorted =
    items === null ? [] : [...items].sort((a, b) => b.total_minor - a.total_minor)
  const total = sorted.reduce((acc, i) => acc + i.total_minor, 0)
  const shares = sorted.map((item) => item.total_minor / total)
  shares[shares.length - 1] = 1 - shares.slice(0, -1).reduce((acc, s) => acc + s, 0)

  return (
    <section className="card breakdown">
      <div className="breakdown-head">
        <span className="breakdown-title">По категориям</span>
        <select
          className="control"
          value={userId}
          onChange={(e) => setUserId(e.target.value)}
          aria-label="Член семьи (аналитика)"
        >
          <option value="">Вся семья</option>
          {users.map((u) => (
            <option key={u.id} value={String(u.id)}>
              {u.first_name}
            </option>
          ))}
        </select>
      </div>

      {error !== null ? (
        <div className="empty">{error}</div>
      ) : items === null ? (
        <div className="empty">Загрузка…</div>
      ) : sorted.length === 0 ? (
        <div className="empty">Нет трат за период</div>
      ) : (
        <>
          <div className="donut-wrap">
            <Donut items={sorted} shares={shares} />
            <div className="donut-center">
              <span className="donut-total">{formatAmount(grandTotal)}</span>
              <span className="donut-caption">за период</span>
            </div>
          </div>
          <div className="legend">
            {sorted.map((item, i) => {
              const slug = item.category as CategorySlug
              return (
                <div className="legend-row" key={item.category}>
                  <span className="dot" style={{ background: CATEGORY_STYLE[slug].color }} />
                  <span className="legend-name">{item.category_name}</span>
                  <span className="legend-amount">{formatAmount(item.total_minor)}</span>
                  <span className="legend-share">{formatShare(shares[i])}</span>
                </div>
              )
            })}
          </div>
        </>
      )}
    </section>
  )
}

function Donut({ items, shares }: { items: SummaryItem[]; shares: number[] }) {
  const radius = 60
  const circumference = 2 * Math.PI * radius
  let offset = 0
  return (
    <svg
      className="donut"
      width={176}
      height={176}
      viewBox="0 0 176 176"
      role="img"
      aria-label="Распределение по категориям"
    >
      <g transform="rotate(-90 88 88)">
        {items.map((item, i) => {
          const slug = item.category as CategorySlug
          const length = shares[i] * circumference
          const dash = length >= circumference ? circumference : length
          const segment = (
            <circle
              key={item.category}
              cx={88}
              cy={88}
              r={radius}
              fill="none"
              stroke={CATEGORY_STYLE[slug].color}
              strokeWidth={28}
              strokeDasharray={`${dash} ${circumference - dash}`}
              strokeDashoffset={-offset}
            />
          )
          offset += length
          return segment
        })}
      </g>
    </svg>
  )
}

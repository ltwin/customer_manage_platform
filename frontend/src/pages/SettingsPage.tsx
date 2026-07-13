import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError, getSettings, updateSettings } from '../api/client'
import type { ChurnThreshold, Settings } from '../api/client'
import { useShell } from '../components/shellContext'

const shootTypeLabels: Record<ChurnThreshold['shoot_type'], string> = {
  portrait: '写真',
  cosplay: 'Cosplay',
  other: '其他',
}

export default function SettingsPage() {
  const navigate = useNavigate()
  const { notify, retryTimezone } = useShell()
  const [settings, setSettings] = useState<Settings | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const [timezone, setTimezone] = useState('Asia/Shanghai')
  const [birthdayLead, setBirthdayLead] = useState(3)
  const [followUp, setFollowUp] = useState(7)
  const [digestHour, setDigestHour] = useState(9)
  const [thresholds, setThresholds] = useState<ChurnThreshold[]>([])

  useEffect(() => {
    setLoading(true)
    setError(null)
    getSettings()
      .then((s) => {
        setSettings(s)
        setTimezone(s.timezone)
        setBirthdayLead(s.birthday_lead_days)
        setFollowUp(s.follow_up_after_days)
        setDigestHour(s.digest_hour)
        setThresholds(s.churn_thresholds)
      })
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setError(err instanceof Error ? err.message : '加载设置失败')
      })
      .finally(() => setLoading(false))
  }, [navigate])

  async function onSave(e: React.FormEvent) {
    e.preventDefault()
    setFormError(null)
    if (!timezone.trim()) {
      setFormError('时区必填')
      return
    }
    if (birthdayLead < 1 || followUp < 1) {
      setFormError('天数须 ≥ 1')
      return
    }
    if (digestHour < 0 || digestHour > 23) {
      setFormError('digest_hour 须在 0-23')
      return
    }
    for (const t of thresholds) {
      if (t.days < 1) {
        setFormError('流失阈值天数须 ≥ 1')
        return
      }
    }
    setSaving(true)
    try {
      const next = await updateSettings({
        timezone: timezone.trim(),
        birthday_lead_days: birthdayLead,
        follow_up_after_days: followUp,
        digest_hour: digestHour,
        churn_thresholds: thresholds,
      })
      setSettings(next)
      notify('设置已保存')
      retryTimezone()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      setFormError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <>
        <header className="topbar">
          <h1>设置</h1>
        </header>
        <main className="content">
          <div className="empty">加载中…</div>
        </main>
      </>
    )
  }

  if (error || !settings) {
    return (
      <>
        <header className="topbar">
          <h1>设置</h1>
        </header>
        <main className="content">
          <div className="form-error" role="alert">
            {error ?? '无数据'}
          </div>
        </main>
      </>
    )
  }

  return (
    <>
      <header className="topbar">
        <div>
          <h1>设置</h1>
          <div className="sub">时区与提醒参数 · 行可缺省时显示默认值</div>
        </div>
      </header>

      <main className="content">
        <form className="card form-stack" onSubmit={onSave}>
          <label>
            账号时区（IANA）
            <input value={timezone} onChange={(e) => setTimezone(e.target.value)} required />
          </label>
          <label>
            生日提前提醒天数
            <input
              type="number"
              min={1}
              value={birthdayLead}
              onChange={(e) => setBirthdayLead(Number(e.target.value))}
            />
          </label>
          <label>
            交付后回访天数
            <input
              type="number"
              min={1}
              value={followUp}
              onChange={(e) => setFollowUp(Number(e.target.value))}
            />
          </label>
          <label>
            每日摘要小时（0-23，按账号时区）
            <input
              type="number"
              min={0}
              max={23}
              value={digestHour}
              onChange={(e) => setDigestHour(Number(e.target.value))}
            />
          </label>

          <fieldset>
            <legend>流失阈值（天）</legend>
            {thresholds.map((entry, index) => (
              <label key={entry.shoot_type}>
                {shootTypeLabels[entry.shoot_type] ?? entry.shoot_type}
                <input
                  type="number"
                  min={1}
                  value={entry.days}
                  onChange={(e) => {
                    const days = Number(e.target.value)
                    setThresholds((prev) =>
                      prev.map((item, i) => (i === index ? { ...item, days } : item)),
                    )
                  }}
                />
              </label>
            ))}
          </fieldset>

          {formError && (
            <div className="form-error" role="alert">
              {formError}
            </div>
          )}

          <div className="topbar-actions">
            <button className="btn btn-primary" type="submit" disabled={saving}>
              {saving ? '保存中…' : '保存'}
            </button>
          </div>
        </form>
      </main>
    </>
  )
}

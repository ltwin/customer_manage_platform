import { useState } from 'react'

import {
  ApiError,
  updateSettings,
  type Settings,
} from '../../api/client.ts'
import { isPackagePriceYuanInputAllowed } from '../../pages/packagePrice.ts'
import {
  businessRuleDescriptors,
  businessRuleDraftFromOverrides,
  businessRuleOverridesFromDraft,
  validateBusinessRuleDraft,
  type BusinessRuleDraft,
  type BusinessRuleKey,
  type BusinessRuleMode,
} from './businessRulesDraft.ts'

export default function PlanningBusinessRulesSection({
  settings,
  disabled,
  onSaved,
  onRefresh,
}: {
  settings: Settings
  disabled: boolean
  onSaved(snapshot: Settings): void
  onRefresh(): void
}) {
  const [draft, setDraft] = useState<BusinessRuleDraft>(() => (
    businessRuleDraftFromOverrides(settings.planning_business_rule_overrides)
  ))
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function setMode(key: BusinessRuleKey, mode: BusinessRuleMode) {
    setDraft((current) => ({
      ...current,
      [key]: {
        mode,
        value: mode === 'value' ? current[key].value : '',
      },
    }))
    setDirty(true)
    setError(null)
  }

  function setValue(key: BusinessRuleKey, value: string, money: boolean) {
    if (money && !isPackagePriceYuanInputAllowed(value)) return
    if (!money && value !== '' && !/^\d+$/.test(value)) return
    setDraft((current) => ({ ...current, [key]: { mode: 'value', value } }))
    setDirty(true)
    setError(null)
  }

  async function save() {
    const validationError = validateBusinessRuleDraft(draft)
    if (validationError) {
      setError(validationError)
      return
    }
    setSaving(true)
    setError(null)
    try {
      const snapshot = await updateSettings({
        planning_business_rules: {
          expected_revision: settings.planning_business_rule_revision,
          overrides: businessRuleOverridesFromDraft(draft),
        },
      })
      setDirty(false)
      onSaved(snapshot)
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 409) {
        setError('经营规则已被其他窗口更新，正在刷新最新版本')
        onRefresh()
      } else {
        setError(reason instanceof Error ? reason.message : '经营规则保存失败')
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="card form-stack planning-business-rules" aria-labelledby="accountSettingsBusinessRulesTitle">
      <div>
        <h2 id="accountSettingsBusinessRulesTitle">经营规则</h2>
        <p className="sub">只影响新生成草稿，不会直接改动订单或档期。</p>
      </div>
      <form onSubmit={(event) => { event.preventDefault(); void save() }}>
        <fieldset className="settings-edit-fields" disabled={disabled || saving}>
          <div className="planning-business-rule-list">
            {businessRuleDescriptors.map((descriptor) => {
              const entry = draft[descriptor.key]
              return (
                <div className="planning-business-rule-row" key={descriptor.key}>
                  <div className="planning-business-rule-label">
                    <strong>{descriptor.label}</strong>
                    <span>{descriptor.unit === 'money' ? '元' : '个'}</span>
                  </div>
                  <div className="segmented planning-business-rule-mode" aria-label={`${descriptor.label}取值方式`}>
                    <ModeButton active={entry.mode === 'inherit'} onClick={() => setMode(descriptor.key, 'inherit')}>继承</ModeButton>
                    <ModeButton active={entry.mode === 'unknown'} onClick={() => setMode(descriptor.key, 'unknown')}>未知</ModeButton>
                    <ModeButton active={entry.mode === 'value'} onClick={() => setMode(descriptor.key, 'value')}>自定义</ModeButton>
                  </div>
                  <input
                    aria-label={`${descriptor.label}${descriptor.unit === 'money' ? '（元）' : ''}`}
                    className="planning-business-rule-value"
                    inputMode={descriptor.unit === 'money' ? 'decimal' : 'numeric'}
                    placeholder={entry.mode === 'value' ? '输入数值' : entry.mode === 'inherit' ? '使用系统默认' : '明确未知'}
                    value={entry.value}
                    disabled={entry.mode !== 'value'}
                    onChange={(event) => setValue(descriptor.key, event.target.value, descriptor.unit === 'money')}
                  />
                </div>
              )
            })}
          </div>
          {error && <div className="form-error" role="alert">{error}</div>}
          <div className="topbar-actions">
            <span className="sub">规则版本 {settings.planning_business_rule_revision}</span>
            <button className="btn btn-primary" type="submit" disabled={disabled || saving || !dirty}>
              {saving ? '保存中…' : '保存经营规则'}
            </button>
          </div>
        </fieldset>
      </form>
    </section>
  )
}

function ModeButton({ active, onClick, children }: {
  active: boolean
  onClick(): void
  children: string
}) {
  return <button type="button" className={active ? 'active' : ''} aria-pressed={active} onClick={onClick}>{children}</button>
}

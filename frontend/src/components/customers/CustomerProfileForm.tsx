import { useState } from 'react'
import { ApiError, updateCustomer } from '../../api/client'
import type { Customer, CustomerDetail, UpdateCustomerBody } from '../../api/client'
import { channelOptions } from '../../pages/customerLabels'
import type { CustomerChannel } from '../../pages/customerLabels'

type Props = {
  customer: CustomerDetail
  onSaved: (updated: Customer) => void
  onCancel: () => void
  onUnauthorized: () => void
}

// 档案字段编辑（design D2/D6）：可清空字段留空即显式传 null；
// channel 改为 referral 必带介绍人，改走 referral 由服务端自动清空介绍人。
export default function CustomerProfileForm({ customer, onSaved, onCancel, onUnauthorized }: Props) {
  const [displayName, setDisplayName] = useState(customer.display_name)
  const [realName, setRealName] = useState(customer.real_name ?? '')
  const [phone, setPhone] = useState(customer.phone ?? '')
  const [birthday, setBirthday] = useState(customer.birthday ?? '')
  const [channel, setChannel] = useState<CustomerChannel>(customer.channel)
  const [referrerCustomerID, setReferrerCustomerID] = useState(customer.referrer_customer_id ?? '')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function buildPatch(): UpdateCustomerBody {
    const body: UpdateCustomerBody = {}
    if (displayName.trim() !== customer.display_name) {
      body.display_name = displayName.trim()
    }
    clearableField(body, 'real_name', realName, customer.real_name)
    clearableField(body, 'phone', phone, customer.phone)
    clearableField(body, 'birthday', birthday, customer.birthday)
    if (channel !== customer.channel) {
      body.channel = channel
    }
    if (channel === 'referral') {
      const referrer = referrerCustomerID.trim()
      if (referrer !== (customer.referrer_customer_id ?? '') || body.channel) {
        body.referrer_customer_id = referrer
        body.channel = 'referral'
      }
    }
    return body
  }

  async function submit() {
    setError(null)
    if (!displayName.trim()) {
      setError('昵称不能为空')
      return
    }
    if (channel === 'referral' && !referrerCustomerID.trim()) {
      setError('渠道为客户介绍时必须填写介绍人客户 ID')
      return
    }
    const body = buildPatch()
    if (Object.keys(body).length === 0) {
      onCancel()
      return
    }
    setSaving(true)
    try {
      const updated = await updateCustomer(customer.id ?? '', body)
      onSaved(updated)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="card">
      <div className="card-title">编辑档案</div>
      {error && <div className="form-error">{error}</div>}
      <div className="field">
        <label htmlFor="edit-display-name">昵称 *</label>
        <input
          id="edit-display-name"
          className="input"
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          autoFocus
        />
      </div>
      <div className="field">
        <label htmlFor="edit-real-name">真实姓名（留空即清除）</label>
        <input
          id="edit-real-name"
          className="input"
          value={realName}
          onChange={(event) => setRealName(event.target.value)}
        />
      </div>
      <div className="field">
        <label htmlFor="edit-phone">手机号（留空即清除）</label>
        <input
          id="edit-phone"
          className="input"
          value={phone}
          onChange={(event) => setPhone(event.target.value)}
        />
      </div>
      <div className="field">
        <label htmlFor="edit-birthday">生日 MM-DD 或 YYYY-MM-DD（留空即清除）</label>
        <input
          id="edit-birthday"
          className="input"
          value={birthday}
          placeholder="03-15"
          onChange={(event) => setBirthday(event.target.value)}
        />
      </div>
      <div className="field">
        <label htmlFor="edit-channel">来源渠道</label>
        <select
          id="edit-channel"
          className="input"
          value={channel}
          onChange={(event) => setChannel(event.target.value as CustomerChannel)}
        >
          {channelOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
        </select>
      </div>
      {channel === 'referral' && (
        <div className="field">
          <label htmlFor="edit-referrer">介绍人客户 ID *（须为经营中客户）</label>
          <input
            id="edit-referrer"
            className="input"
            value={referrerCustomerID}
            onChange={(event) => setReferrerCustomerID(event.target.value)}
          />
        </div>
      )}
      <div className="topbar-actions">
        <button className="btn" type="button" onClick={onCancel} disabled={saving}>取消</button>
        <button className="btn btn-primary" type="button" onClick={submit} disabled={saving}>
          {saving ? '保存中…' : '保存'}
        </button>
      </div>
    </section>
  )
}

// clearableField：与原值比对——改值传新值、清空传 null、没动不传（null 三态，design D9）。
function clearableField(
  body: UpdateCustomerBody,
  key: 'real_name' | 'phone' | 'birthday',
  input: string,
  original: string | null | undefined,
) {
  const trimmed = input.trim()
  const before = original ?? ''
  if (trimmed === before) return
  body[key] = trimmed === '' ? null : trimmed
}

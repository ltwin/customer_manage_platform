import { useState } from 'react'
import { ApiError, addCustomerIdentity, deleteCustomerIdentity } from '../../api/client'
import type { CustomerDetail } from '../../api/client'
import { platformLabels, platformOptions } from '../../pages/customerLabels'
import type { SocialPlatform } from '../../pages/customerLabels'

type Props = {
  customer: CustomerDetail
  onChanged: () => void
  onUnauthorized: () => void
}

// 社交身份管理（design D3）：增删走档案端点；删除最后一个身份被服务端
// 以 409 last_identity 拒绝，这里把封套 message 原样展示。
export default function IdentitySection({ customer, onChanged, onUnauthorized }: Props) {
  const [adding, setAdding] = useState(false)
  const [platform, setPlatform] = useState<SocialPlatform>('wechat')
  const [handle, setHandle] = useState('')
  const [remark, setRemark] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function submitAdd() {
    setError(null)
    if (!handle.trim()) {
      setError('账号不能为空')
      return
    }
    setBusy(true)
    try {
      await addCustomerIdentity(customer.id ?? '', {
        platform,
        handle: handle.trim(),
        remark: remark.trim() || undefined,
      })
      setAdding(false)
      setHandle('')
      setRemark('')
      onChanged()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof Error ? err.message : '添加失败')
    } finally {
      setBusy(false)
    }
  }

  async function remove(identityId: string) {
    setError(null)
    setBusy(true)
    try {
      await deleteCustomerIdentity(customer.id ?? '', identityId)
      onChanged()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof Error ? err.message : '删除失败')
    } finally {
      setBusy(false)
    }
  }

  const readonly = customer.status === 'merged'

  return (
    <section className="card">
      <div className="card-title">私域账号 <span className="count">· {customer.identities.length}</span></div>
      {error && <div className="form-error">{error}</div>}
      {customer.identities.map((identity) => (
        <div className="identity-item identity-account-item" key={identity.id}>
          <span className="plat">{platformLabels[identity.platform]}</span>
          <span className="handle">{identity.handle}</span>
          <span className="rmk">{identity.remark}</span>
          {!readonly && (
            <button
              className="btn btn-danger identity-remove"
              type="button"
              onClick={() => { void remove(identity.id ?? '') }}
              disabled={busy}
            >
              删除
            </button>
          )}
        </div>
      ))}
      {!readonly && !adding && (
        <button className="btn btn-ghost" type="button" onClick={() => setAdding(true)}>＋ 添加账号</button>
      )}
      {!readonly && adding && (
        <div className="identity-row">
          <div className="field">
            <label htmlFor="new-identity-platform">平台</label>
            <select
              id="new-identity-platform"
              className="input"
              value={platform}
              onChange={(event) => setPlatform(event.target.value as SocialPlatform)}
            >
              {platformOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="new-identity-handle">账号 / ID *</label>
            <input
              id="new-identity-handle"
              className="input"
              value={handle}
              onChange={(event) => setHandle(event.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor="new-identity-remark">备注</label>
            <input
              id="new-identity-remark"
              className="input"
              value={remark}
              onChange={(event) => setRemark(event.target.value)}
            />
          </div>
          <div className="topbar-actions">
            <button className="btn" type="button" onClick={() => setAdding(false)} disabled={busy}>取消</button>
            <button className="btn btn-primary" type="button" onClick={() => { void submitAdd() }} disabled={busy}>保存</button>
          </div>
        </div>
      )}
    </section>
  )
}

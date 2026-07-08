import { Link, useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { ApiError, createCustomer, listCustomers } from '../api/client'
import type { CustomerListResponse } from '../api/client'
import { channelOptions, platformOptions } from './customerLabels'
import type { CustomerChannel, SocialPlatform } from './customerLabels'
import { useShell } from '../components/shellContext'

type IdentityDraft = {
  platform: SocialPlatform
  handle: string
  remark: string
}

type ReferrerOption = CustomerListResponse['items'][number]

const blankIdentity = (): IdentityDraft => ({ platform: 'wechat', handle: '', remark: '' })

export default function CustomerNewPage() {
  const navigate = useNavigate()
  const { notify } = useShell()
  const [displayName, setDisplayName] = useState('')
  const [channel, setChannel] = useState<CustomerChannel>('xiaohongshu')
  const [referrerCustomerID, setReferrerCustomerID] = useState('')
  const [referrerOptions, setReferrerOptions] = useState<ReferrerOption[]>([])
  const [referrerLoading, setReferrerLoading] = useState(false)
  const [referrerError, setReferrerError] = useState<string | null>(null)
  const [identities, setIdentities] = useState<IdentityDraft[]>([blankIdentity()])
  const [submitted, setSubmitted] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  function updateIdentity(index: number, patch: Partial<IdentityDraft>) {
    setIdentities((current) => current.map((identity, itemIndex) => (
      itemIndex === index ? { ...identity, ...patch } : identity
    )))
  }

  function addIdentity() {
    setIdentities((current) => [...current, blankIdentity()])
  }

  function removeIdentity(index: number) {
    setIdentities((current) => current.length === 1 ? current : current.filter((_, itemIndex) => itemIndex !== index))
  }

  useEffect(() => {
    if (channel !== 'referral') {
      setReferrerCustomerID('')
      setReferrerError(null)
      return
    }
    let active = true
    setReferrerLoading(true)
    setReferrerError(null)
    listCustomers({ status: 'active', page: 1, pageSize: 100 })
      .then((result) => {
        if (!active) return
        setReferrerOptions(result.items)
        setReferrerCustomerID((current) => (
          result.items.some((customer) => customer.id === current) ? current : ''
        ))
      })
      .catch((err: unknown) => {
        if (!active) return
        if (err instanceof ApiError && err.status === 401) {
          navigate('/login', { replace: true })
          return
        }
        setReferrerOptions([])
        setReferrerError(err instanceof Error ? err.message : '介绍人列表加载失败')
      })
      .finally(() => {
        if (active) setReferrerLoading(false)
      })
    return () => {
      active = false
    }
  }, [channel, navigate])

  async function submit() {
    setSubmitted(true)
    setError(null)
    if (
      !displayName.trim()
      || identities.some((identity) => !identity.handle.trim())
      || (channel === 'referral' && !referrerCustomerID)
    ) return
    setSaving(true)
    try {
      const created = await createCustomer({
        display_name: displayName.trim(),
        channel,
        referrer_customer_id: channel === 'referral' ? referrerCustomerID.trim() || undefined : undefined,
        identities: identities.map((identity) => ({
          platform: identity.platform,
          handle: identity.handle.trim(),
          remark: identity.remark.trim() || undefined,
        })),
      })
      notify(`已建档「${created.display_name}」`)
      navigate(`/customers/${created.id}`)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        navigate('/login', { replace: true })
        return
      }
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const referrerPlaceholder = referrerLoading
    ? '正在加载介绍人…'
    : referrerOptions.length === 0
      ? '暂无可选介绍人'
      : '请选择介绍人'

  return (
    <>
      <header className="topbar">
        <div>
          <div className="crumb"><Link to="/customers">客户</Link> / 新建</div>
          <h1>30 秒建档</h1>
        </div>
        <div className="topbar-actions">
          <Link className="btn" to="/customers">取消</Link>
          <button className="btn btn-primary" type="button" onClick={submit} disabled={saving}>保存并打开档案</button>
        </div>
      </header>

      <main className="content">
        <section className="card new-customer-form">
          {error && <div className="form-error">{error}</div>}
          <div className={`field${submitted && !displayName.trim() ? ' show-err' : ''}`}>
            <label htmlFor="displayName">昵称 *</label>
            <input
              id="displayName"
              className={`input${submitted && !displayName.trim() ? ' invalid' : ''}`}
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              autoFocus
            />
            <div className="err">昵称不能为空</div>
          </div>

          <div className="field">
            <label htmlFor="channel">来源渠道</label>
            <select id="channel" className="input" value={channel} onChange={(event) => setChannel(event.target.value as CustomerChannel)}>
              {channelOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
            </select>
          </div>

          {channel === 'referral' && (
            <div className={`field${submitted && !referrerCustomerID ? ' show-err' : ''}`}>
              <label htmlFor="referrerCustomerID">介绍人</label>
              <select
                id="referrerCustomerID"
                className={`input${submitted && !referrerCustomerID ? ' invalid' : ''}`}
                value={referrerCustomerID}
                onChange={(event) => setReferrerCustomerID(event.target.value)}
                disabled={referrerLoading || referrerOptions.length === 0}
              >
                <option value="">{referrerPlaceholder}</option>
                {referrerOptions.map((customer) => (
                  <option key={customer.id} value={customer.id}>
                    {formatCustomerSelectLabel(customer)}
                  </option>
                ))}
              </select>
              <div className="err">请选择介绍人</div>
              {referrerError && <div className="hint danger-text">{referrerError}</div>}
            </div>
          )}

          <div className="identity-editor">
            <div className="card-title">私域账号 <span className="count">· {identities.length}</span></div>
            {identities.map((identity, index) => (
              <div className="identity-row" key={index}>
                <div className="field">
                  <label htmlFor={`platform-${index}`}>平台</label>
                  <select
                    id={`platform-${index}`}
                    className="input"
                    value={identity.platform}
                    onChange={(event) => updateIdentity(index, { platform: event.target.value as SocialPlatform })}
                  >
                    {platformOptions.map(([key, label]) => <option key={key} value={key}>{label}</option>)}
                  </select>
                </div>
                <div className={`field${submitted && !identity.handle.trim() ? ' show-err' : ''}`}>
                  <label htmlFor={`handle-${index}`}>账号 / ID *</label>
                  <input
                    id={`handle-${index}`}
                    className={`input${submitted && !identity.handle.trim() ? ' invalid' : ''}`}
                    value={identity.handle}
                    onChange={(event) => updateIdentity(index, { handle: event.target.value })}
                  />
                  <div className="err">账号不能为空</div>
                </div>
                <div className="field">
                  <label htmlFor={`remark-${index}`}>备注</label>
                  <input
                    id={`remark-${index}`}
                    className="input"
                    value={identity.remark}
                    onChange={(event) => updateIdentity(index, { remark: event.target.value })}
                  />
                </div>
                <button
                  className="btn btn-ghost identity-remove"
                  type="button"
                  onClick={() => removeIdentity(index)}
                  disabled={identities.length === 1}
                >
                  删除
                </button>
              </div>
            ))}
            <button className="btn btn-ghost" type="button" onClick={addIdentity}>＋ 添加账号</button>
          </div>
        </section>
      </main>
    </>
  )
}

function formatCustomerSelectLabel(customer: ReferrerOption) {
  return `${customer.display_name} · UID ${shortCustomerID(customer.id ?? '')}`
}

function shortCustomerID(id: string) {
  return `#${id.replace(/^cus_/, '').slice(-6)}`
}

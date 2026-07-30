import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  AlertTriangle,
  Bell,
  CalendarDays,
  Camera,
  Package,
  Sparkles,
  Users,
} from 'lucide-react'
import './WelcomePage.css'

type PreviewTab = 'customers' | 'calendar' | 'packages'

const previewTabs: { key: PreviewTab; label: string; icon: typeof Users }[] = [
  { key: 'customers', label: '客户档案', icon: Users },
  { key: 'calendar', label: '档期日历', icon: CalendarDays },
  { key: 'packages', label: '拍摄套系', icon: Package },
]

export default function WelcomePage() {
  const [tab, setTab] = useState<PreviewTab>('customers')

  function onTabKeyDown(event: React.KeyboardEvent<HTMLButtonElement>, index: number) {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return
    event.preventDefault()
    const delta = event.key === 'ArrowRight' ? 1 : previewTabs.length - 1
    const next = previewTabs[(index + delta) % previewTabs.length]
    setTab(next.key)
    document.getElementById(`wp-tab-${next.key}`)?.focus()
  }

  return (
    <div className="wp">
      <nav className="wp-topnav" data-od-id="topnav">
        <div className="wp-wrap">
          <a className="wp-brand" href="#top">
            <span className="brand-mark" aria-hidden="true">
              <Camera strokeWidth={2} />
            </span>
            <span>
              影约 CRM<small>摄影师私域客户经营</small>
            </span>
          </a>
          <div className="wp-nav-links">
            <a href="#value">解决什么</a>
            <a href="#preview">界面预览</a>
            <a href="#flow">全链路</a>
          </div>
          <div className="wp-nav-actions">
            <Link className="btn btn-ghost" to="/login" data-od-id="nav-login">
              登录
            </Link>
            <a className="btn btn-primary" href="#cta" data-od-id="nav-apply">
              申请开通
            </a>
          </div>
        </div>
      </nav>

      <main id="top">
        {/* Hero */}
        <section className="wp-hero" data-od-id="hero">
          <div className="wp-wrap wp-hero-grid">
            <div>
              <span className="wp-eyebrow">
                <Sparkles aria-hidden="true" />
                为独立摄影师做的经营台
              </span>
              <h1>
                客户记在聊天里，
                <br />
                档期记在<em>脑子里</em>，
                <br />
                到头来都记不住。
              </h1>
              <p className="wp-lede">
                影约把客户档案、约单、档期、套系收进一套系统。从私域来的第一句咨询，到交片后的复购提醒，全链路只用一个工作台。
              </p>
              <div className="wp-hero-cta">
                <Link className="btn btn-primary btn-lg" to="/login" data-od-id="hero-login">
                  登录经营台
                </Link>
                <a className="btn btn-lg" href="#preview" data-od-id="hero-preview">
                  先看看界面
                </a>
              </div>
              <div className="wp-hero-meta">
                <span>
                  <i className="wp-dot" aria-hidden="true" /> 数据存在你自己的账号里
                </span>
                <span>
                  <b>微信 / QQ / Telegram</b> 私域客户统一归档
                </span>
              </div>
            </div>

            <div className="wp-hero-visual">
              <div className="wp-hero-photo">
                <img src="/marketing/hero-studio.jpg" alt="柔光影棚一角，木凳上放着一束干花" />
              </div>
              <div className="wp-float-card" data-od-id="hero-float-card">
                <div className="wp-fc-top">
                  <img src="/marketing/av-1.jpg" alt="" />
                  <div>
                    <div className="wp-fc-name">林小满</div>
                    <div className="wp-fc-sub">Cos 返图第 3 次复购</div>
                  </div>
                </div>
                <div className="wp-fc-row">
                  <span>
                    生日 <b className="num">03-14</b> · 还有 9 天
                  </span>
                  <span className="wp-tag wp-tag-pink">该问候了</span>
                </div>
              </div>
            </div>
          </div>
        </section>

        {/* 价值主张 */}
        <section className="wp-band" id="value" data-od-id="value-section">
          <div className="wp-wrap">
            <div className="wp-sec-head">
              <span className="wp-eyebrow">替你省下的事</span>
              <h2>不是又一个记账本，是替你记住细节的那个人</h2>
              <p>三件最容易翻车的事，交给系统来盯。</p>
            </div>
            <div className="wp-value-grid">
              <article className="wp-value-card" data-od-id="value-card-profile">
                <div className="wp-value-icon" aria-hidden="true">
                  <Users strokeWidth={1.9} />
                </div>
                <h3>客户画像自己长出来</h3>
                <p>
                  社交账号、手机号、生日、拍摄偏好，随每一单自动沉淀。谁是老客、谁快流失，档案页一眼看得出。
                </p>
                <p className="wp-before">
                  <s>翻三个月前的聊天记录找生日</s> → <b>打开档案就有</b>
                </p>
              </article>

              <article className="wp-value-card" data-od-id="value-card-schedule">
                <div className="wp-value-icon" aria-hidden="true">
                  <CalendarDays strokeWidth={1.9} />
                </div>
                <h3>档期冲突保存前就拦下</h3>
                <p>
                  约单直接落在日历格子上，同一时段撞档、跨时区标注、临时改期的连锁影响，系统当场提示，不等到客户到场才发现。
                </p>
                <p className="wp-before">
                  <s>两个客户约到同一个下午</s> → <b>提交时就被拦住</b>
                </p>
              </article>

              <article className="wp-value-card" data-od-id="value-card-remind">
                <div className="wp-value-icon" aria-hidden="true">
                  <Bell strokeWidth={1.9} />
                </div>
                <h3>该跟进的人一个都不漏</h3>
                <p>
                  定金未付、精修超期、拍完 30 天该回访，到点自动生成提醒，并推到你的 Telegram。跟进闭环，不靠便利贴。
                </p>
                <p className="wp-before">
                  <s>想起来才发消息，一想起就晚了</s> → <b>到点推到手机</b>
                </p>
              </article>
            </div>
          </div>
        </section>

        {/* 界面预览 */}
        <section id="preview" data-od-id="preview-section">
          <div className="wp-wrap">
            <div className="wp-sec-head">
              <span className="wp-eyebrow">真实界面</span>
              <h2>三个每天都要开的页面</h2>
              <p>下面是系统里的实际布局，不是效果图。切换看看客户、档期、套系。</p>
            </div>

            <div className="wp-preview-tabs" role="tablist" aria-label="界面预览">
              {previewTabs.map((item, index) => {
                const Icon = item.icon
                return (
                  <button
                    key={item.key}
                    type="button"
                    role="tab"
                    id={`wp-tab-${item.key}`}
                    className="wp-ptab"
                    aria-selected={tab === item.key}
                    aria-controls={`wp-panel-${item.key}`}
                    onClick={() => setTab(item.key)}
                    onKeyDown={(event) => onTabKeyDown(event, index)}
                    data-od-id={`ptab-${item.key}`}
                  >
                    <Icon aria-hidden="true" strokeWidth={1.9} />
                    {item.label}
                  </button>
                )
              })}
            </div>

            {tab === 'customers' && (
              <div
                className="wp-screen"
                role="tabpanel"
                id="wp-panel-customers"
                aria-labelledby="wp-tab-customers"
                data-od-id="screen-customers"
              >
                <div className="wp-screen-bar">
                  <span className="wp-lights" aria-hidden="true">
                    <i />
                    <i />
                    <i />
                  </span>
                  <span className="wp-path">影约 CRM / 客户</span>
                </div>
                <div className="wp-screen-body">
                  <div className="wp-screen-head">
                    <div>
                      <h4>客户列表</h4>
                      <p>按最近互动排序，活跃度与待跟进状态直接标在行上。</p>
                    </div>
                    <span className="wp-tag wp-tag-accent">共 148 位客户</span>
                  </div>

                  <div className="wp-kpis">
                    <div className="wp-kpi">
                      <div className="wp-k-label">本月新客</div>
                      <div className="wp-k-val">12</div>
                      <div className="wp-k-foot">较上月 +3</div>
                    </div>
                    <div className="wp-kpi">
                      <div className="wp-k-label">复购客户</div>
                      <div className="wp-k-val wp-pink">37</div>
                      <div className="wp-k-foot">占比 25%</div>
                    </div>
                    <div className="wp-kpi">
                      <div className="wp-k-label">待跟进</div>
                      <div className="wp-k-val">6</div>
                      <div className="wp-k-foot">含 2 条已超期</div>
                    </div>
                    <div className="wp-kpi">
                      <div className="wp-k-label">近 30 天沉默</div>
                      <div className="wp-k-val">21</div>
                      <div className="wp-k-foot">建议问候</div>
                    </div>
                  </div>

                  <div className="wp-tbl-scroll">
                    <table className="wp-tbl">
                      <thead>
                        <tr>
                          <th>客户</th>
                          <th>渠道</th>
                          <th>累计约单</th>
                          <th>最近一单</th>
                          <th>状态</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr>
                          <td>
                            <div className="wp-cust">
                              <img src="/marketing/av-1.jpg" alt="" />
                              <div>
                                <b>林小满</b>
                                <small>@xiaoman_cos</small>
                              </div>
                            </div>
                          </td>
                          <td>Telegram</td>
                          <td className="num">3</td>
                          <td className="num">02-28</td>
                          <td>
                            <span className="wp-tag wp-tag-pink">生日临近</span>
                          </td>
                        </tr>
                        <tr>
                          <td>
                            <div className="wp-cust">
                              <img src="/marketing/av-2.jpg" alt="" />
                              <div>
                                <b>周迟</b>
                                <small>wx: chichi_0714</small>
                              </div>
                            </div>
                          </td>
                          <td>微信</td>
                          <td className="num">5</td>
                          <td className="num">03-02</td>
                          <td>
                            <span className="wp-tag wp-tag-ok">已交片</span>
                          </td>
                        </tr>
                        <tr>
                          <td>
                            <div className="wp-cust">
                              <img src="/marketing/av-3.jpg" alt="" />
                              <div>
                                <b>苏念</b>
                                <small>QQ: 872****15</small>
                              </div>
                            </div>
                          </td>
                          <td>QQ</td>
                          <td className="num">1</td>
                          <td className="num">03-05</td>
                          <td>
                            <span className="wp-tag wp-tag-warn">定金未付</span>
                          </td>
                        </tr>
                        <tr>
                          <td>
                            <div className="wp-cust">
                              <img src="/marketing/av-4.jpg" alt="" />
                              <div>
                                <b>何以安</b>
                                <small>@anan_studio</small>
                              </div>
                            </div>
                          </td>
                          <td>Telegram</td>
                          <td className="num">2</td>
                          <td className="num">01-19</td>
                          <td>
                            <span className="wp-tag wp-tag-accent">待回访</span>
                          </td>
                        </tr>
                        <tr>
                          <td>
                            <div className="wp-cust">
                              <span className="wp-initial" aria-hidden="true">
                                陈
                              </span>
                              <div>
                                <b>陈屿</b>
                                <small>wx: yu_frames</small>
                              </div>
                            </div>
                          </td>
                          <td>微信</td>
                          <td className="num">4</td>
                          <td className="num">03-08</td>
                          <td>
                            <span className="wp-tag wp-tag-warn">精修中</span>
                          </td>
                        </tr>
                      </tbody>
                    </table>
                  </div>
                </div>
              </div>
            )}

            {tab === 'calendar' && (
              <div
                className="wp-screen"
                role="tabpanel"
                id="wp-panel-calendar"
                aria-labelledby="wp-tab-calendar"
                data-od-id="screen-calendar"
              >
                <div className="wp-screen-bar">
                  <span className="wp-lights" aria-hidden="true">
                    <i />
                    <i />
                    <i />
                  </span>
                  <span className="wp-path">影约 CRM / 档期 / 3 月 14 日</span>
                </div>
                <div className="wp-screen-body">
                  <div className="wp-screen-head">
                    <div>
                      <h4>今日档期</h4>
                      <p>每个时段绑定客户与套系；冲突在保存前拦下。</p>
                    </div>
                    <span className="wp-tag wp-tag-accent">3 场已确认 · 1 场待定</span>
                  </div>

                  <div className="wp-cal">
                    <div className="wp-cal-time">09:00</div>
                    <div className="wp-cal-lane">
                      <div className="wp-slot wp-done">
                        <b>周迟 · 城市清晨写真</b>
                        <span>09:00–11:00 · 外景 · 已收全款</span>
                      </div>
                    </div>

                    <div className="wp-cal-time">13:00</div>
                    <div className="wp-cal-lane">
                      <div className="wp-slot">
                        <b>林小满 · Cosplay 双套换装</b>
                        <span>13:00–17:00 · 棚拍 · 定金已收</span>
                      </div>
                    </div>

                    <div className="wp-cal-time">15:00</div>
                    <div className="wp-cal-lane">
                      <div className="wp-slot wp-conflict">
                        <b>苏念 · 情绪人像</b>
                        <span>15:00–17:00 · 与上一场重叠 2 小时</span>
                      </div>
                    </div>

                    <div className="wp-cal-time">19:00</div>
                    <div className="wp-cal-lane">
                      <div className="wp-slot wp-hold">
                        <b>何以安 · 夜景约拍</b>
                        <span>19:00–21:00 · 待客户确认 · 占位保留至今晚</span>
                      </div>
                    </div>
                  </div>

                  <div className="wp-conflict-note">
                    <AlertTriangle aria-hidden="true" strokeWidth={2.2} />
                    <span>
                      <b>档期冲突</b>：苏念 15:00–17:00 与林小满 13:00–17:00
                      重叠。可改期、缩短时长，或标记为助理跟拍后再保存。
                    </span>
                  </div>
                </div>
              </div>
            )}

            {tab === 'packages' && (
              <div
                className="wp-screen"
                role="tabpanel"
                id="wp-panel-packages"
                aria-labelledby="wp-tab-packages"
                data-od-id="screen-packages"
              >
                <div className="wp-screen-bar">
                  <span className="wp-lights" aria-hidden="true">
                    <i />
                    <i />
                    <i />
                  </span>
                  <span className="wp-path">影约 CRM / 套系</span>
                </div>
                <div className="wp-screen-body">
                  <div className="wp-screen-head">
                    <div>
                      <h4>拍摄套系</h4>
                      <p>张数、时长、底片与精修数量写死在套系上，报价不再靠临场心算。</p>
                    </div>
                    <span className="wp-tag wp-tag-accent">6 个在售套系</span>
                  </div>

                  <div className="wp-pkg-grid">
                    <article className="wp-pkg" data-od-id="pkg-city-morning">
                      <div className="wp-pkg-cover">
                        <img src="/marketing/work-1.jpg" alt="外景人像样片" loading="lazy" />
                        <span className="wp-pkg-badge">写真</span>
                      </div>
                      <div className="wp-pkg-body">
                        <h5>城市清晨写真</h5>
                        <div className="wp-pkg-price">
                          ¥ 880 <small>/ 单人</small>
                        </div>
                        <ul className="wp-pkg-specs">
                          <li>
                            拍摄时长 <b>2h</b>
                          </li>
                          <li>
                            底片交付 <b>120 张</b>
                          </li>
                          <li>
                            精修 <b>12 张</b>
                          </li>
                        </ul>
                      </div>
                    </article>

                    <article className="wp-pkg" data-od-id="pkg-cos-double">
                      <div className="wp-pkg-cover">
                        <img src="/marketing/work-2.jpg" alt="棚拍人像样片" loading="lazy" />
                        <span className="wp-pkg-badge">Cosplay</span>
                      </div>
                      <div className="wp-pkg-body">
                        <h5>Cosplay 双套换装</h5>
                        <div className="wp-pkg-price">
                          ¥ 1,680 <small>/ 单人</small>
                        </div>
                        <ul className="wp-pkg-specs">
                          <li>
                            拍摄时长 <b>4h</b>
                          </li>
                          <li>
                            底片交付 <b>260 张</b>
                          </li>
                          <li>
                            精修 <b>25 张</b>
                          </li>
                        </ul>
                      </div>
                    </article>

                    <article className="wp-pkg" data-od-id="pkg-mood-night">
                      <div className="wp-pkg-cover">
                        <img src="/marketing/hero-studio.jpg" alt="影棚布光样片" loading="lazy" />
                        <span className="wp-pkg-badge">情绪片</span>
                      </div>
                      <div className="wp-pkg-body">
                        <h5>夜色情绪人像</h5>
                        <div className="wp-pkg-price">
                          ¥ 1,280 <small>/ 单人</small>
                        </div>
                        <ul className="wp-pkg-specs">
                          <li>
                            拍摄时长 <b>3h</b>
                          </li>
                          <li>
                            底片交付 <b>180 张</b>
                          </li>
                          <li>
                            精修 <b>18 张</b>
                          </li>
                        </ul>
                      </div>
                    </article>
                  </div>
                </div>
              </div>
            )}
          </div>
        </section>

        {/* 全链路 */}
        <section className="wp-band" id="flow" data-od-id="flow-section">
          <div className="wp-wrap">
            <div className="wp-sec-head">
              <span className="wp-eyebrow">全链路</span>
              <h2>从私域第一句咨询，到交片后的下一次复购</h2>
              <p>五个阶段串成一条线，每一步都留下可查的记录。</p>
            </div>
            <div className="wp-flow">
              <article className="wp-step" data-od-id="step-lead">
                <h4>接到咨询</h4>
                <p>微信、QQ、Telegram 来的人先建档，社交账号即身份，不怕重名。</p>
                <div className="wp-who">
                  <i aria-hidden="true" />
                  客户模块
                </div>
              </article>
              <article className="wp-step" data-od-id="step-quote">
                <h4>报价选套系</h4>
                <p>直接套用已定价套系，张数时长自动带出，加项单独计。</p>
                <div className="wp-who">
                  <i aria-hidden="true" />
                  套系模块
                </div>
              </article>
              <article className="wp-step" data-od-id="step-book">
                <h4>锁定档期</h4>
                <p>约单落进日历，绑定客户与套系，冲突当场拦下，定金状态跟着单走。</p>
                <div className="wp-who">
                  <i aria-hidden="true" />
                  档期模块
                </div>
              </article>
              <article className="wp-step" data-od-id="step-deliver">
                <h4>拍摄与交付</h4>
                <p>精修进度、交片时间写在单上，超期自动进提醒队列。</p>
                <div className="wp-who">
                  <i aria-hidden="true" />
                  约单模块
                </div>
              </article>
              <article className="wp-step" data-od-id="step-repeat">
                <h4>回访与复购</h4>
                <p>交片 30 天回访、生日问候到点提醒，老客自然接上下一单。</p>
                <div className="wp-who">
                  <i aria-hidden="true" />
                  提醒引擎
                </div>
              </article>
            </div>
          </div>
        </section>

        {/* CTA */}
        <section id="cta" data-od-id="cta-section">
          <div className="wp-wrap">
            <div className="wp-cta-card">
              <span className="wp-eyebrow">现在开始</span>
              <h2>下一单之前，先把上一单的客户记住</h2>
              <p>已开通的账号直接登录；还没有账号的话，说明你的拍摄类型与客户量，我们手动为你开通。</p>
              <div className="wp-cta-actions">
                <Link className="btn btn-primary btn-lg" to="/login" data-od-id="cta-login">
                  登录经营台
                </Link>
                <Link className="btn btn-lg" to="/login" data-od-id="cta-apply">
                  申请开通账号
                </Link>
              </div>
              <p className="wp-cta-fine">
                客户资料仅存于你自己的账号，不做跨账号共享，可随时整包导出。
              </p>
            </div>
          </div>
        </section>
      </main>

      <footer className="wp-footer" data-od-id="footer">
        <div className="wp-wrap wp-foot-grid">
          <a className="wp-brand" href="#top">
            <span className="brand-mark" aria-hidden="true">
              <Camera strokeWidth={2} />
            </span>
            <span>
              影约 CRM<small>摄影师私域客户经营</small>
            </span>
          </a>
          <div className="wp-foot-links">
            <a href="#value">解决什么</a>
            <a href="#preview">界面预览</a>
            <a href="#flow">全链路</a>
            <Link to="/login">登录</Link>
          </div>
          <p className="wp-foot-note">摄影师私域客户经营系统 · 自部署</p>
        </div>
      </footer>
    </div>
  )
}

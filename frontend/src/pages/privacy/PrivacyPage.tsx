// 隐私说明（初版）：公开页面，不需要登录。创作助手的首次外发说明链接到这里。
// 不列具体的模型服务商（owner 2026-10-11，PRD §11.3 #13）：摄影师只选模型，
// 由哪家提供是平台的路由，每次调用实际发往哪家记在调用记录里。
import { Link } from 'react-router-dom'
import './privacy.css'

const updatedAt = '2026-10-11'

export default function PrivacyPage() {
  return (
    <main className="privacy">
      <article>
        <header>
          <p className="privacy-eyebrow">隐私说明 · 初版</p>
          <h1>你的内容会去哪里</h1>
          <p className="privacy-date">更新于 {updatedAt}</p>
        </header>

        <section aria-labelledby="privacy-scope">
          <h2 id="privacy-scope">这份说明管什么</h2>
          <p>
            客户档案、订单、档期等经营数据只保存在本平台，不会发送给任何模型服务商。只有在你使用创意空间的创作助手或图片生成时，相关内容才会发送给平台签约的模型服务商处理。
          </p>
        </section>

        <section aria-labelledby="privacy-assistant">
          <h2 id="privacy-assistant">创作助手会发送什么</h2>
          <ul>
            <li>你写的文字，以及选择的 Skill。</li>
            <li>你附带或 @ 引用的图片（压缩后的预览图）。</li>
            <li>
              助手完成任务时读取的当前画布内容：节点标题、文字、生成提示词和参数。
            </li>
          </ul>
          <p>
            助手会为完成任务按需查找资产库，发送相关素材的标题、描述、标签、分组及它查看的图片预览，也可能再次读取本次对话里之前出现过的内容。第一次发送前会弹出说明；说明有变化时会再提示一次。
          </p>
        </section>

        <section aria-labelledby="privacy-generation">
          <h2 id="privacy-generation">图片生成会发送什么</h2>
          <p>
            节点上的生成提示词、参数，以及你在节点上选中的参考图（最多 9 张）。
          </p>
        </section>

        <section aria-labelledby="privacy-processors">
          <h2 id="privacy-processors">谁会处理这些内容</h2>
          <p>
            平台签约的模型服务商。同一个模型可能由多家服务商提供，平台会按可用情况自动选择，你不需要也无法指定。每次调用实际发给了哪一家，都会记录在平台的调用记录里。
          </p>
          <p>
            图片生成的结果会先存放在服务商的文件服务上，可通过链接公开访问，至少保留
            3 天；平台取回之后无法删除服务商那里的副本。
          </p>
        </section>

        <section aria-labelledby="privacy-more">
          <h2 id="privacy-more">更新</h2>
          <p>
            发送范围或处理方式变化时，这份说明会同步更新。账号与登录安全相关的设置见
            <Link to="/account/security">隐私与安全</Link>。
          </p>
        </section>
      </article>
    </main>
  )
}

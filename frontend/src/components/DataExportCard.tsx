import { useState } from 'react'

import { ApiError } from '../api/client.ts'
import { requestAndDownloadDataExport } from './dataExportDownload.ts'

type Props = {
	onUnauthorized: () => void
}

export default function DataExportCard({ onUnauthorized }: Props) {
	const [downloading, setDownloading] = useState(false)
	const [error, setError] = useState<string | null>(null)

	async function onExport() {
		if (downloading) return
		setDownloading(true)
		setError(null)
		try {
			await requestAndDownloadDataExport()
		} catch (error) {
			if (error instanceof ApiError && error.status === 401) {
				onUnauthorized()
				return
			}
			setError('导出失败，请重试。未完整读取的文件不会保存。')
		} finally {
			setDownloading(false)
		}
	}

	return (
		<section className="card form-stack" aria-labelledby="dataExportTitle">
			<div>
				<h2 id="dataExportTitle">导出全部 JSON 数据</h2>
				<p className="sub">
					文件包含姓名、手机号、社交身份、备注、Telegram chat ID 等敏感信息，
					只应保存到受控位置，并在不再需要时及时删除。
				</p>
				<p className="sub">
					头像图片未包含；文件只有当前系统的公开头像引用，不可跨部署恢复头像。
				</p>
			</div>
			{error && (
				<div className="form-error" role="alert">
					{error}
				</div>
			)}
			<div className="topbar-actions">
				<button className="btn btn-primary" type="button" disabled={downloading} onClick={onExport}>
					{downloading ? '正在准备导出…' : '导出全部 JSON 数据'}
				</button>
			</div>
		</section>
	)
}

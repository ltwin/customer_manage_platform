import { useState } from 'react'

import { ApiError } from '../api/client.ts'
import { EXPORT_SECTION } from '../account/security/securityCopy.ts'
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
			setError(EXPORT_SECTION.failure)
		} finally {
			setDownloading(false)
		}
	}

	return (
		<section className="card form-stack" aria-labelledby="dataExportTitle">
			<div>
				<h2 id="dataExportTitle">{EXPORT_SECTION.title}</h2>
				<p className="sub">{EXPORT_SECTION.piiHint}</p>
				<p className="sub">{EXPORT_SECTION.avatarHint}</p>
			</div>
			{error && (
				<div className="form-error" role="alert">
					{error}
				</div>
			)}
			<div className="topbar-actions">
				<button className="btn btn-primary" type="button" disabled={downloading} onClick={onExport}>
					{downloading ? EXPORT_SECTION.preparing : EXPORT_SECTION.button}
				</button>
			</div>
		</section>
	)
}

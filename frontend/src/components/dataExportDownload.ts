import { fetchDataExport } from '../api/client.ts'
import type { DataExportDownload } from '../api/client.ts'

export function saveDataExport(result: DataExportDownload): void {
	const objectURL = URL.createObjectURL(result.blob)
	try {
		const anchor = document.createElement('a')
		anchor.href = objectURL
		anchor.download = result.filename
		anchor.click()
	} finally {
		URL.revokeObjectURL(objectURL)
	}
}

export async function requestAndDownloadDataExport(): Promise<void> {
	const result = await fetchDataExport()
	saveDataExport(result)
}

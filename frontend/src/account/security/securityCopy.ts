/** 隐私与安全最终 IA 文案（account-privacy-security；不新增 security API）。 */

export const IDENTITY_SECTION = {
	title: '登录身份',
	subtitle: '邮箱只读；换绑邮箱不在本版范围。',
	emailLabel: '登录邮箱',
	verifiedLabel: '验证状态',
	verifiedValue: '已验证',
} as const

export const SECURITY_SECTION = {
	title: '账号安全',
	subtitle: '修改密码会撤销所有设备的刷新会话；退出登录只撤销当前会话。',
	changePassword: '修改密码',
	logout: '退出登录',
	loggingOut: '正在退出…',
} as const

export const EXPORT_SECTION = {
	title: '导出全部 JSON 数据',
	piiHint:
		'文件包含姓名、手机号、社交身份、备注、Telegram chat ID 等敏感信息，只应保存到受控位置，并在不再需要时及时删除。',
	avatarHint:
		'头像图片未包含。账号资料仅含头像元数据（version、media_type、size、updated_at），不含 avatar_url、对象 ID 或可跨部署恢复的字节引用。',
	button: '导出全部 JSON 数据',
	preparing: '正在准备导出…',
	failure: '导出失败，请重试。未完整读取的文件不会保存。',
} as const

export const BOUNDARY_SECTION = {
	title: '本版边界说明',
	body:
		'本版不提供设备中心、查看或退出其他设备、换绑邮箱、登录历史、安全事件、OAuth／SSO、手机号身份、删除账号，或彻底擦除备份。' +
		'头像移除后约 24 小时内由线上 GC 清理当前对象；历史备份仍可能保留旧对象，且完整数据导出不含头像字节。' +
		'改密、退出与导出已集中在用户中心「隐私与安全」；旧「设置」页不再提供这些入口。',
} as const

export const PASSWORD_PAGE = {
	title: '修改登录密码',
	subtitle: '修改成功后会撤销所有设备的刷新会话，并返回登录页。',
	backToSecurity: '返回隐私与安全',
} as const

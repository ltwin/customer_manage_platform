// 项目就地改名的规则，项目页卡片和画布外框（V6-044）共用。
// Enter 或失焦提交、Esc 取消由调用方处理；这里只决定提交什么。

// 与服务端一致：按 Unicode 码位计数，不超过 200。
export const projectNameLimit = 200

export type ProjectNameEdit =
  { kind: 'keep' } | { kind: 'rename'; name: string } | { kind: 'too-long' }

// 去掉首尾空白后为空或与原名相同，就保留原名，不发请求。
export function decideProjectName(
  input: string,
  current: string,
): ProjectNameEdit {
  const name = input.trim()
  if (!name || name === current) return { kind: 'keep' }
  if ([...name].length > projectNameLimit) return { kind: 'too-long' }
  return { kind: 'rename', name }
}

export const projectNameMessages = {
  tooLong: `名称最多 ${projectNameLimit} 个字`,
  conflict: '项目已在别处改过',
}

import { lazy } from 'react'

// 导入模型对话框：换算 GLB 要用 three.js，按需加载，不进资产库页面的主包。
const ImportModelDialog = lazy(() => import('../../creative-scene/import/ImportModelDialog.tsx'))
export default ImportModelDialog

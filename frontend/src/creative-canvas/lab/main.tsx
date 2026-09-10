import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ReactFlowProvider } from '@xyflow/react'
import CanvasLab from './CanvasLab.tsx'
import '@xyflow/react/dist/style.css'
import './style.css'

if (import.meta.env.DEV) {
  createRoot(document.getElementById('root')!).render(<StrictMode><ReactFlowProvider><CanvasLab /></ReactFlowProvider></StrictMode>)
} else { document.body.textContent = '此验证页仅在开发环境开放。' }

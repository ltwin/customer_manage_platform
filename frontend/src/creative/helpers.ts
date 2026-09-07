import { useEffect, useState } from 'react'
export function useOnline() {
  const [online, setOnline] = useState(navigator.onLine)
  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => {
      window.removeEventListener('online', update)
      window.removeEventListener('offline', update)
    }
  }, [])
  return online
}
export function message(error: unknown) {
  if (error instanceof TypeError) return '未能连接服务器，输入已保留，请联网后重试。'
  return error instanceof Error
    ? error.message
    : '保存失败，请重试。输入仍保留在这里。'
}

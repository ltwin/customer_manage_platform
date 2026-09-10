// 同源画布侧栏变体；独立 v4 页面不启用，不切换其存储或视觉。
if (window.parent !== window && new URLSearchParams(location.search).get('surface') === 'canvas') {
  document.documentElement.dataset.surface = 'canvas';
  const link = document.createElement('link'); link.rel = 'stylesheet'; link.href = '../v5/shelf.css'; document.head.append(link);
}

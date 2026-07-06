# A14 二进制直跑证据（review-fix REV-002，2026-07-06 04:52:41）

## 进程
41777 backend/bin/server

## 启动日志（migrate → ensure → 监听）
{"time":"2026-07-06T04:52:18.89696-07:00","level":"INFO","msg":"HTTP 监听","addr":":8080"}
{"time":"2026-07-06T04:52:19.962479-07:00","level":"INFO","msg":"request","method":"GET","path":"/healthz","status":200,"duration":1335959}
{"time":"2026-07-06T04:52:19.968562-07:00","level":"INFO","msg":"request","method":"GET","path":"/","status":200,"duration":98875}

## GET /healthz
{"status":"ok"}

## GET /（SPA index，由二进制 go:embed 托管）
    <script type="module" crossorigin src="/assets/index-C0BJWqWf.js"></script>
    <link rel="stylesheet" crossorigin href="/assets/index-kVdPmZtQ.css">

## GET /login（深链 SPA fallback，非 404）
HTTP 200 content-type=text/html; charset=utf-8

## 产物指纹：served asset == frontend/dist asset（证明静态由本次构建的 embed 提供）
served=2226abcbf0ec887e56769f6596ffe270
frontend/dist=2226abcbf0ec887e56769f6596ffe270
MATCH

## Vite dev server 未运行（排除 5173 来源混淆）
000 5173 无监听

## 截图来源核对：无头 Chrome 访问 http://localhost:8080/ 时二进制的请求日志（含 assets 命中）
{"time":"2026-07-06T04:52:41.455752-07:00","level":"INFO","msg":"request","method":"GET","path":"/assets/index-C0BJWqWf.js","status":200,"duration":1999292}
{"time":"2026-07-06T04:53:07.267501-07:00","level":"INFO","msg":"request","method":"GET","path":"/","status":200,"duration":52167}
{"time":"2026-07-06T04:53:07.308205-07:00","level":"INFO","msg":"request","method":"GET","path":"/assets/index-kVdPmZtQ.css","status":200,"duration":25750}
{"time":"2026-07-06T04:53:07.308241-07:00","level":"INFO","msg":"request","method":"GET","path":"/assets/index-C0BJWqWf.js","status":200,"duration":151709}
{"time":"2026-07-06T04:53:07.354086-07:00","level":"INFO","msg":"request","method":"GET","path":"/favicon.svg","status":200,"duration":138125}
{"time":"2026-07-06T04:53:08.552436-07:00","level":"INFO","msg":"request","method":"GET","path":"/favicon.svg","status":200,"duration":42708}

截图文件 a14-binary-embed-login.png 由上述访问产生（headless Chrome，1280x800）；与 a13-3 不再同一文件：
MD5 (.codestable/features/2026-07-06-platform-skeleton/evidence/a14-binary-embed-login.png) = 9a66df5cba62e317f1f75db61b23f503
MD5 (.codestable/features/2026-07-06-platform-skeleton/evidence/a13-3-guard-redirect-login.png) = fb186e1f95fada7a33603283b90aee10

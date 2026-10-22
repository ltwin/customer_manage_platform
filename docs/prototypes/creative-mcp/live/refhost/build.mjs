// 打包参考宿主（浏览器里跑，需要把 SDK 打成一个文件）
import { build } from 'esbuild'
import { fileURLToPath } from 'node:url'
const here = (p) => fileURLToPath(new URL(p, import.meta.url))
await build({ entryPoints: [here('./refhost.mjs')], bundle: true, format: 'esm', platform: 'browser', target: 'es2022', outfile: here('../.build/refhost.js'), logLevel: 'warning' })
console.log('已生成 .build/refhost.js')

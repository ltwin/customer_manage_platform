// W5 spike: a page served from the scratchpad, three from the frontend's node_modules (symlinked).
import path from 'node:path'
const here = import.meta.dirname
export default {
  root: here,
  cacheDir: path.join(here, '.vite-cache'),
  server: { fs: { allow: [here, '/Users/samson/workspace/my_project/customer_manage_platform/frontend'] } },
  resolve: { preserveSymlinks: false },
  logLevel: 'info',
}

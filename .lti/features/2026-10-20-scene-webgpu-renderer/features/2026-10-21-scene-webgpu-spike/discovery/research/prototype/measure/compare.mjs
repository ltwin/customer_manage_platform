// Difference of two PNG data URLs in the page: mean absolute difference per
// channel (0-255), the share of pixels where any channel differs by more than
// 8, the largest difference, and a difference image amplified 8 times.
export const compareInPage = (page, a, b) =>
  page.evaluate(async ([a, b]) => {
    const load = (src) => new Promise((resolve) => { const i = new Image(); i.onload = () => resolve(i); i.src = src })
    const [x, y] = await Promise.all([load(a), load(b)])
    if (x.width !== y.width || x.height !== y.height) throw new Error(`sizes differ: ${x.width}×${x.height} and ${y.width}×${y.height}`)
    const c = document.createElement('canvas')
    c.width = x.width
    c.height = x.height
    const g = c.getContext('2d', { willReadFrequently: true })
    g.drawImage(x, 0, 0)
    const p = g.getImageData(0, 0, c.width, c.height).data
    g.clearRect(0, 0, c.width, c.height)
    g.drawImage(y, 0, 0)
    const q = g.getImageData(0, 0, c.width, c.height)
    const d = q.data
    let sum = 0
    let over = 0
    let max = 0
    for (let i = 0; i < p.length; i += 4) {
      let pixelMax = 0
      for (let k = 0; k < 3; k++) {
        const v = Math.abs(p[i + k] - d[i + k])
        sum += v
        if (v > pixelMax) pixelMax = v
        d[i + k] = Math.min(255, v * 8)
      }
      d[i + 3] = 255
      if (pixelMax > 8) over++
      if (pixelMax > max) max = pixelMax
    }
    g.putImageData(q, 0, 0)
    const pixels = p.length / 4
    return { mean: sum / (pixels * 3), over8: over / pixels, max, diff: c.toDataURL('image/png') }
  }, [a, b])

export const savePNG = (fs, path, url) => fs.writeFileSync(path, Buffer.from(url.split(',')[1], 'base64'))

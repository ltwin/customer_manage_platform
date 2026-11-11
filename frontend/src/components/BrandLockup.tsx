// Eurika 品牌标志。图形来自品牌设计稿：32px 及以下用简化点阵星，48px 起用点阵展示版；
// 字标用 currentColor 描边，跟随所在区域的文字色适配明暗主题。

const BRAND_NAME = 'Eurika'

type BrandWordmarkProps = {
  className?: string
  // 设计稿描边是 8（字标高 180 时）。小字号下按光学尺寸加粗，避免细线发虚。
  strokeWidth?: number
}

export function BrandWordmark({ className, strokeWidth = 8 }: BrandWordmarkProps) {
  return (
    <svg
      className={className}
      viewBox="6 42 608 166"
      role="img"
      aria-label={BRAND_NAME}
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M14 150 H106 A46 46 0 1 0 95.2 179.6 M142 100 V150 A46 46 0 0 0 234 150 V100 M234 100 V200 M270 100 V200 M270 150 A46 46 0 0 1 339 110.2 M375 100 V200 M411 50 V200 M477 100 L411 164 M439 137 L481 200 M605 150 A46 46 0 1 1 605 149.9 M605 100 V200" />
      <path
        d="M375 49 Q376.5 66.5 394 68 Q376.5 69.5 375 87 Q373.5 69.5 356 68 Q373.5 66.5 375 49Z"
        fill="currentColor"
        stroke="none"
      />
    </svg>
  )
}

// 横版：侧栏、顶栏等 28px 品牌位。
export function BrandLockup({ subtitle = '摄影师私域客户经营' }: { subtitle?: string }) {
  return (
    <>
      <img className="brand-mark" src="/brand/mark.svg" alt="" aria-hidden="true" />
      <span className="brand-name">
        <BrandWordmark className="brand-wordmark" strokeWidth={13} />
        <small>{subtitle}</small>
      </span>
    </>
  )
}

// 竖版：欢迎页、登录页的大号品牌位，带中文名「有光」。
export function BrandLockupStacked() {
  return (
    <span className="brand-stacked">
      <img className="brand-stacked-mark" src="/brand/mark-display.svg" alt="" aria-hidden="true" />
      <BrandWordmark className="brand-stacked-wordmark" strokeWidth={10} />
      <span className="brand-stacked-cn">有光</span>
    </span>
  )
}

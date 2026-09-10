/** 先按元信息布局，再加载图片；图片解码不改变几何位置。 */
export function pack(items, containerWidth, { compact = false, mobile = false, minimumMobileWidth = 140 } = {}) {
  const gap = mobile ? 12 : 20;
  const columns = mobile ? (containerWidth < minimumMobileWidth * 2 ? 1 : 2) : Math.max(2, Math.min(6, Math.floor((containerWidth + gap) / ((compact ? 180 : 220) + gap))));
  const width = (containerWidth - (columns - 1) * gap) / columns;
  const heights = Array(columns).fill(0);
  const positions = items.map(item => {
    const column = heights.indexOf(Math.min(...heights));
    const height = width * item.height / item.width + (mobile ? 57 : 62);
    const position = { x: column * (width + gap), y: heights[column], width, height };
    heights[column] += height + gap;
    return position;
  });
  return { positions, height: Math.max(0, ...heights) - (items.length ? gap : 0), columns };
}

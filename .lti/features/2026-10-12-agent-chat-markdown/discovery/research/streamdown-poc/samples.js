export const sample = String.raw`## 拍摄方案

**暖色逆光**搭配 *低饱和背景*，参考 [拍摄说明](https://example.com)。~~旧方案~~ 已更新。

> 尽量在日落前 40 分钟开始，保留环境层次。

- [x] 确认场地
- [ ] 准备灯光
  - 主光柔光箱
  - 背景补光

### 预算与安排

| 项目 | 用途 | 费用 | 详细说明 |
| :--- | :--- | ---: | :--- |
| 场地 | 室内人像 | ¥300 | 北向落地窗，注意室外背景曝光 |
| 灯光 | 轮廓补光 | ¥150 | 柔光箱与蜂巢，可按需调整 |

### 曝光公式

行内：$$E = mc^2$$。美元金额：$5 和 $10。

$$
\\frac{1}{f} = \\frac{1}{u} + \\frac{1}{v}
$$

### 执行流程

\`\`\`mermaid
graph TD
 A[确认主题] --> B{天气适合吗}
 B -->|是| C[户外逆光拍摄]
 B -->|否| D[室内柔光拍摄]
 C --> E[选片与交付]
 D --> E
\`\`\`

### 参数示例

\`\`\`typescript
const plan = { aperture: 2.8, iso: 100 };
console.log(plan);
\`\`\`

方案结束。`.replaceAll('\\`','`').replaceAll('\\\\','\\');
export const diagram = '```mermaid\ngraph TD\n A[确认主题] --> B[拍摄]\n B --> C[选片]\n C --> D[交付]\n```';
export const cases = {
  complete: sample,
  table: '| 项目 | 详细参数 | 备注 |\n| --- | --- | --- |\n| A | ' + 'VeryLongUnbrokenWord'.repeat(8) + ' | 正常 |',
  formula: '$$\\frac{1}{2}+\\sqrt{x}$$\n\n$$\n'+Array(22).fill('x^2').join('+')+'\n$$',
  badMath: '$$\\unknownCommand{x}$$',
  badMermaid: '```mermaid\ngraph THIS IS NOT VALID\n A --->> {\n```',
  partial: '**尚未闭合\n\n```mermaid\ngraph LR\n A-->B',
  security: '<script>window.__xss=1</script>\n\n[bad](javascript:alert(1))\n\n<img src="https://example.com/tracker.png" onerror="window.__xss=2">',
  text: '## 普通消息\n\n这是一条只有 **中文内容** 和列表的回复。\n\n- 第一项\n- 第二项',
};

import { createMathPlugin } from '@streamdown/math'
import 'katex/dist/katex.min.css'
// $$ works inline and on separate lines; single dollars remain currency.
export default createMathPlugin({ singleDollarTextMath: false })

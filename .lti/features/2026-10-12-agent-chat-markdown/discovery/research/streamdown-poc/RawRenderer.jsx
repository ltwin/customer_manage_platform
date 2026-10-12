import React from 'react';
import {Streamdown} from 'streamdown';
import {math} from '@streamdown/math';
import {mermaid} from '@streamdown/mermaid';
import {code} from '@streamdown/code';
import 'katex/dist/katex.min.css';
const plugins={math,mermaid,code};
export default function RawRenderer({source,streaming}) {return <Streamdown className="cc-agent-text" plugins={plugins} isAnimating={streaming} mode={streaming?'streaming':'static'}>{source}</Streamdown>}

import React, {memo, useEffect, useMemo, useRef, useState} from 'react';
import {Streamdown} from 'streamdown';
import './adapter.css';
let seq=0;
const Diagram = memo(function Diagram({code: source,isIncomplete}) {
  const [svg,setSVG]=useState(''),[error,setError]=useState(''),[copied,setCopied]=useState(false);
  const dialog=useRef(null);
  useEffect(()=>{
    let stale=false;setSVG('');setError('');
    if(isIncomplete)return;
    window.__diagramRequests=(window.__diagramRequests||0)+1;
    const id=`poc-diagram-${++seq}`;
    import('@streamdown/mermaid').then(async({mermaid})=>{
      const engine=mermaid.getMermaid({theme:'dark',securityLevel:'strict',suppressErrorRendering:true});
      const result=await engine.render(id,source);
      if(!stale)setSVG(result.svg);
    }).catch(err=>{if(!stale)setError(String(err))});
    return ()=>{stale=true};
  },[source,isIncomplete]);
  return <section className="diagram" data-incomplete={isIncomplete}>
    <div className="diagram-bar"><span>Mermaid</span><button onClick={()=>navigator.clipboard.writeText(source).then(()=>setCopied(true))}>{copied?'已复制':'复制源码'}</button>{svg&&<button onClick={()=>dialog.current.showModal()}>放大</button>}</div>
    {svg?<div className="diagram-image" dangerouslySetInnerHTML={{__html:svg}}/>:<div className="diagram-fallback"><small>{isIncomplete?'图表正在接收…':error?'图表语法错误，保留源码':'图表加载中…'}</small><pre>{source}</pre></div>}
    <dialog ref={dialog}><button onClick={()=>dialog.current.close()}>关闭</button><div dangerouslySetInnerHTML={{__html:svg}}/></dialog>
  </section>
});
const renderers=[{language:'mermaid',component:Diagram}];
const cache={};
const loading={};
const loaders={math:()=>import('./MathPlugin'),code:()=>import('./CodePlugin')};
function load(name) {
 if(!loading[name]) loading[name]=loaders[name]().then(module=>cache[name]=module.default);
 return loading[name];
}
const components={a:({children,href})=><a href={href} target="_blank" rel="noopener noreferrer">{children}</a>,img:({alt})=><span>[图片：{alt||'外部图片未加载'}]</span>};
export default function Renderer({source,streaming}) {
 const [loaded,setLoaded]=useState({...cache});
 const needsMath=source.includes('$$');
 const needsCode=/^\s*(?:`{3,}|~{3,})[ \t]*(?:typescript|javascript|json|python|go)\b/m.test(source);
 useEffect(()=>{let stale=false;for(const name of [needsMath?'math':null,needsCode?'code':null].filter(Boolean)) void load(name).then(plugin=>{if(!stale)setLoaded(previous=>({...previous,[name]:plugin}))});return ()=>{stale=true}},[needsMath,needsCode]);
 const plugins=useMemo(()=>({...loaded,renderers}),[loaded]);
 return <Streamdown className="cc-agent-text" plugins={plugins} components={components} controls={{code:{copy:true,download:false},table:false}} mode="streaming" parseIncompleteMarkdown={streaming} isAnimating={streaming} skipHtml>{source}</Streamdown>
}

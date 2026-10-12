// THROWAWAY validation harness; not a product route.
import React, { Suspense, lazy, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { sample, cases } from './samples';
import './project-tokens.css';
import './project-agent.css';
import './harness.css';
const Adapted = lazy(() => import('./Renderer'));
const Raw = lazy(() => import('./RawRenderer'));
const params = new URLSearchParams(location.search);
const raw = params.get('raw') === '1';
const fixedScroll = params.get('scrollfix') !== '0';
function App() {
  const [source, setSource] = useState(cases[params.get('case')] || sample);
  const [streaming, setStreaming] = useState(false);
  const [opened,setOpened] = useState(params.get('closed') !== '1');
  const log=useRef(null), body=useRef(null), pinned=useRef(true), timer=useRef(), programmaticTop=useRef(null);
  const stick=()=> { if(log.current && pinned.current) {log.current.scrollTop=log.current.scrollHeight;programmaticTop.current=log.current.scrollTop;} };
  useEffect(()=>{ requestAnimationFrame(stick); },[source,opened]);
  useEffect(()=>{
    if(!fixedScroll || !body.current) return;
    const observer=new ResizeObserver(()=>requestAnimationFrame(stick));
    observer.observe(body.current);return ()=>observer.disconnect();
  },[opened]);
  useEffect(()=>{
    window.poc={
      set: (value,live=false)=>{clearInterval(timer.current);setSource(value);setStreaming(live);},
      open:()=>setOpened(true),
      stream: (value,step=8,delay=15)=>{clearInterval(timer.current);let i=0;setSource('');setStreaming(true);timer.current=setInterval(()=>{i+=step;setSource(value.slice(0,i));if(i>=value.length){clearInterval(timer.current);setStreaming(false)}},delay)},
      stop:()=>{clearInterval(timer.current);setStreaming(false)},
      sample,
    };
    return ()=>clearInterval(timer.current);
  },[]);
  const View=raw?Raw:Adapted;
  return <main className="cc-workspace">
    <section className="intro"><small>隔离实验 · 非产品页面</small><h1>Agent Markdown 验证</h1><p>React 19 · Streamdown 2.7 · 原项目深色 token</p><p>{raw?'直接接入：未适配控件样式':'局部适配：普通 CSS / 按需图表 / 原生放大层'}</p><button onClick={()=>window.poc.stream(sample)}>重播流式回复</button> <button onClick={()=>window.poc.stop()}>停止</button><p><a href="?raw=1">直接接入</a> · <a href="?">局部适配</a></p><p>流式状态：<output id="status">{streaming?'streaming':'complete'}</output></p></section>
    {!opened?<button onClick={()=>setOpened(true)}>打开助手</button>:<aside className="cc-agent-panel">
      <header><div>创作助手<small>表格 · 公式 · Mermaid</small></div><span>验证预览</span></header>
      <div className="cc-agent-history" ref={log} onScroll={e=>{const el=e.currentTarget;if(fixedScroll && programmaticTop.current!==null && Math.abs(el.scrollTop-programmaticTop.current)<1)return;pinned.current=el.scrollHeight-el.scrollTop-el.clientHeight<48}}>
        <div ref={body} className={raw?'raw':'adapted'}><Suspense fallback={<p>加载渲染器…</p>}><View source={source} streaming={streaming}/></Suspense></div>
      </div><footer>仅验证渲染，无网络模型请求</footer>
    </aside>}
  </main>
}
createRoot(document.getElementById('root')).render(<App/>);

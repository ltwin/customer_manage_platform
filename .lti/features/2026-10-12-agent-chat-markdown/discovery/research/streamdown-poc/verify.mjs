import {chromium} from 'playwright';
import {mkdir,writeFile,readFile,stat,readdir} from 'node:fs/promises';
import {gzipSync} from 'node:zlib';
import assert from 'node:assert/strict';
import {sample,cases,diagram} from './samples.js';
await mkdir('evidence',{recursive:true});
const browser=await chromium.launch({headless:true});
const results=[],errors=[],observations=[];
async function observe(name,fn){const data=await fn();observations.push({name,data});console.log("OBSERVE",name,JSON.stringify(data));}
async function test(name,fn){try {const data=await fn();results.push({name,pass:true,data});console.log('PASS',name,JSON.stringify(data??''));} catch(e){results.push({name,pass:false,error:e.stack});console.log('FAIL',name,e.message)}}
async function open(query='',viewport={width:1100,height:950}) {
 const context=await browser.newContext({viewport,permissions:['clipboard-read','clipboard-write']});
 const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.goto('http://127.0.0.1:4179/'+query);await page.waitForFunction(()=>window.poc);
 return {page,context};
}
async function geometry(page){return page.locator('.cc-agent-history').evaluate(el=>({client:el.clientWidth,scroll:el.scrollWidth,overflow:el.scrollWidth-el.clientWidth,bottomGap:Math.round(el.scrollHeight-el.scrollTop-el.clientHeight)}))}
await test('complete: table, math, nested list, code, Mermaid',async()=>{
 const {page,context}=await open();
 await page.locator('.diagram-image svg').waitFor();await page.locator('pre code span').first().waitFor();
 assert.equal(await page.locator('table tbody tr').count(),2);
 assert.equal(await page.locator('.katex').count(),2);
 assert.equal(await page.locator('input[type=checkbox]').count(),2);
 assert.equal(await page.locator('ul ul').count(),1);
 const colors=await page.locator('pre code span[style]').evaluateAll(els=>[...new Set(els.map(el=>getComputedStyle(el).color))]);assert.ok(colors.length>=3,'syntax colors are visible');
 const lineDisplay=await page.locator('pre code>span').first().evaluate(el=>getComputedStyle(el).display);assert.equal(lineDisplay,'block');
 await page.locator('[data-streamdown=code-block-copy-button]').click();assert.ok((await page.evaluate(()=>navigator.clipboard.readText())).includes('console.log'));
 const g=await geometry(page);assert.ok(g.overflow<=1);
 await page.locator('.cc-agent-history').evaluate(el=>el.scrollTop=0);
 await page.screenshot({path:'evidence/adapted-top.png'});
 await page.locator('.diagram').scrollIntoViewIfNeeded();await page.screenshot({path:'evidence/adapted-diagram.png'});
 await page.getByRole('button',{name:'复制源码'}).click();
 assert.ok((await page.evaluate(()=>navigator.clipboard.readText())).includes('graph TD'));
 await page.getByRole('button',{name:'放大',exact:true}).click();
 assert.equal(await page.locator('dialog').evaluate(el=>el.matches(':modal')),true);
 await page.screenshot({path:'evidence/diagram-expanded.png'});
 await page.keyboard.press('Escape');assert.equal(await page.locator('dialog').evaluate(el=>el.open),false);
 await context.close();return g;
});
await test('narrow 375px: table and formula stay within panel',async()=>{
 const {page,context}=await open('?case=table',{width:375,height:812});
 await page.locator('table').waitFor();const table=await geometry(page);assert.ok(table.overflow<=1);
 await page.evaluate(value=>window.poc.set(value),cases.formula);await page.locator('.katex-display').waitFor();
 const formula=await geometry(page);assert.ok(formula.overflow<=1);
 const formulaScroll=await page.locator('.katex-display').evaluate(el=>({client:el.clientWidth,scroll:el.scrollWidth}));assert.ok(formulaScroll.scroll>formulaScroll.client);
 await page.evaluate(value=>window.poc.set(value),sample);await page.locator('.diagram-image svg').waitFor();
 await page.locator('.cc-agent-history').evaluate(el=>el.scrollTop=0);await page.screenshot({path:'evidence/mobile-top.png'});
 await context.close();return {table,formula,formulaScroll};
});
await test('stream: Mermaid renders only after closing fence and not on later tokens',async()=>{
 const {page,context}=await open('?case=text');await page.locator('h2').waitFor();
 await page.evaluate(()=>window.__diagramRequests=0);
 const unfinished=diagram.slice(0,-3);
 for(let i=1;i<=unfinished.length;i+=4) await page.evaluate(s=>window.poc.set(s,true),unfinished.slice(0,i));
 await page.evaluate(s=>window.poc.set(s,true),unfinished);await page.waitForTimeout(250);
 assert.equal(await page.locator('.diagram[data-incomplete=true]').count(),1);
 assert.equal(await page.evaluate(()=>window.__diagramRequests),0);
 await page.evaluate(s=>window.poc.set(s,true),diagram);await page.locator('.diagram-image svg').waitFor();
 assert.equal(await page.evaluate(()=>window.__diagramRequests),1);
 await page.locator('.diagram-image svg').evaluate(el=>el.setAttribute('data-probe','stable'));
 for(let i=1;i<=20;i++) await page.evaluate(s=>window.poc.set(s,true),diagram+'\n\n'+('后续文字'.repeat(i)));
 assert.equal(await page.evaluate(()=>window.__diagramRequests),1);
 assert.equal(await page.locator('.diagram-image svg').getAttribute('data-probe'),'stable');
 await page.evaluate(s=>window.poc.set(s,false),diagram+'\n\n完成');await page.waitForTimeout(300);
 const settled=await page.evaluate(()=>window.__diagramRequests);
 await context.close();return {requestsWhileOpen:0,requestsAfterFence:1,requestsAfterSettled:settled};
});
await test('stream: complete sample, one-character chunks',async()=>{
 const {page,context}=await open('?case=text');
 await page.evaluate(s=>window.poc.stream(s,1,4),sample);
 await page.waitForFunction(()=>document.getElementById('status').textContent==='complete',{},{timeout:20000});
 await page.locator('.diagram-image svg').waitFor();assert.equal(await page.locator('table tbody tr').count(),2);assert.equal(await page.locator('.katex').count(),2);
 const text=await page.locator('.cc-agent-text').innerText();assert.ok(text.includes('方案结束'));assert.ok(text.includes('$5 和 $10'));
 await context.close();return {chars:sample.length};
});
await test('invalid Mermaid and math preserve content without page crash',async()=>{
 const {page,context}=await open('?case=badMermaid');await page.getByText('图表语法错误，保留源码').waitFor();
 assert.ok((await page.locator('.diagram-fallback pre').innerText()).includes('NOT VALID'));
 await page.evaluate(s=>window.poc.set(s),cases.badMath);await page.waitForTimeout(200);
 const text=await page.locator('.cc-agent-text').innerText();assert.ok(text.includes('unknownCommand'));
 await context.close();return {badMathText:text};
});
await observe('stopped incomplete fence behavior',async()=>{
 const {page,context}=await open('?case=text');await page.locator('h2').waitFor();
 await page.evaluate(s=>window.poc.set(s,true),cases.partial);await page.locator('.diagram[data-incomplete=true]').waitFor();
 await page.evaluate(()=>window.poc.stop());await page.waitForTimeout(600);
 const data={incomplete:await page.locator('.diagram').getAttribute('data-incomplete'),svg:await page.locator('.diagram-image svg').count(),text:await page.locator('.cc-agent-text').innerText()};
 await context.close();return data;
});
await test('HTML and dangerous URL are inert',async()=>{
 const {page,context}=await open('?case=security');await page.waitForSelector('.cc-agent-text');
 assert.equal(await page.evaluate(()=>window.__xss),undefined);assert.equal(await page.locator('.cc-agent-text script').count(),0);
 assert.equal(await page.locator('.cc-agent-text [href^="javascript:"]').count(),0);assert.equal(await page.locator('.cc-agent-text img').count(),0);
 await context.close();
});
await test('async diagram growth: original scroll versus ResizeObserver',async()=>{
 const out=[];
 for(const fix of [false,true]){
  const {page,context}=await open('?case=text&scrollfix='+(fix?'1':'0'),{width:700,height:600});
  await page.locator('h2').waitFor();
  // Delay only lazy module response to make post-message growth deterministic.
  await page.route('**/assets/*.js',async route=>{await new Promise(r=>setTimeout(r,160));await route.continue()});
  await page.evaluate(s=>window.poc.set(s),sample);await page.locator('.diagram-image svg').waitFor();await page.waitForTimeout(300);
  const g=await geometry(page);out.push({fix,...g});
  if(fix)assert.ok(g.bottomGap<=2);
  await page.locator('.cc-agent-history').evaluate(el=>el.scrollTop=0);await page.waitForTimeout(100);
  await page.evaluate(s=>window.poc.set(s),sample+'\n\n新增文字');await page.waitForTimeout(200);
  assert.equal(await page.locator('.cc-agent-history').evaluate(el=>el.scrollTop),0);
  await context.close();
 }
 return out;
});
await test('cold network stages: closed, plain text, math, code, Mermaid',async()=>{
 const {page,context}=await open('?closed=1&case=text');
 const stages=[];
 async function measure(label){await page.waitForTimeout(350);const resources=await page.evaluate(()=>performance.getEntriesByType('resource').map(r=>r.name).filter(u=>u.includes('/assets/')));let js=0,gzip=0,css=0,fonts=0;const files=[...new Set(resources.map(u=>u.split('/assets/')[1]))];for(const file of files){const bytes=await readFile('dist/assets/'+file);if(file.endsWith('.js')){js+=bytes.length;gzip+=gzipSync(bytes).length}else if(file.endsWith('.css'))css+=bytes.length;else fonts+=bytes.length}stages.push({label,files,jsBytes:js,jsGzipBytes:gzip,cssBytes:css,fontBytes:fonts})}
 await measure('closed');await page.evaluate(()=>window.poc.open());await page.locator('h2').waitFor();await measure('text');
 await page.evaluate(s=>window.poc.set(s),cases.formula);await page.locator('.katex').first().waitFor();await measure('math');
 await page.evaluate(s=>window.poc.set(s),'```typescript\nconst x = 1\n```');await page.locator('pre code span').first().waitFor();await measure('code');
 await page.evaluate(s=>window.poc.set(s),diagram);await page.locator('.diagram-image svg').waitFor();await measure('mermaid');
 await context.close();return stages;
});
await test('long streaming response remains interactive',async()=>{
 const {page,context}=await open('?case=text');await page.locator('h2').waitFor();
 const long=Array.from({length:120},(_,i)=>'### 拍摄建议 '+i+'\n\n'+('注意光线与背景层次，保持自然表达。'.repeat(10))+'\n\n').join('');
 await page.evaluate(()=>{window.__longTasks=[];window.__observer=new PerformanceObserver(list=>window.__longTasks.push(...list.getEntries().map(e=>e.duration)));window.__observer.observe({type:'longtask',buffered:false})});
 const begin=Date.now();await page.evaluate(s=>window.poc.stream(s,160,10),long);
 await page.waitForFunction(()=>document.getElementById('status').textContent==='complete',{},{timeout:20000});
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 const timings=await page.evaluate(()=>({longTasks:window.__longTasks.length,maxLongTask:Math.max(0,...window.__longTasks)}));
 assert.equal(await page.locator('h3').count(),120);
 const g=await geometry(page);assert.ok(g.overflow<=1);
 assert.ok(g.bottomGap<=2, 'long response follows bottom');
 await context.close();return {chars:long.length,wallMs:Date.now()-begin,...timings,...g};
});
await browser.close();
const assets=await readdir('dist/assets');let total=0;for(const f of assets)total+=(await stat('dist/assets/'+f)).size;
await writeFile('evidence/results.json',JSON.stringify({date:'2026-10-12',browser:'Playwright Chromium',results,observations,errors,assets:{count:assets.length,totalBytes:total}},null,2));
console.log(JSON.stringify({passed:results.filter(r=>r.pass).length,failed:results.filter(r=>!r.pass).length,errors,assets:{count:assets.length,totalBytes:total}}));
if(results.some(r=>!r.pass)||errors.length)process.exitCode=1;

// Real composer + clipboard + upload protocol; HTTP fixtures never call a vendor.
import assert from 'node:assert/strict'
import { readFile, writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'
const base = process.env.CREATIVE_AGENT_URL || 'http://127.0.0.1:5193'
const fixture = new URL('../.asset-lookup-qa.html', import.meta.url)
const version = /egressNoticeVersion = '([^']+)'/.exec(await readFile(new URL('../src/creative-canvas/editor/agentState.ts', import.meta.url), 'utf8'))[1]
await writeFile(fixture, `<html><body><div id="root" class="cc-workspace"></div><script type="module">
import React from 'react';import {createRoot} from 'react-dom/client';
import '/src/index.css';import '/src/creative-canvas/editor/workspace.css';
import AgentPanel from '/src/creative-canvas/editor/AgentPanel.tsx';import {setAuthenticated} from '/src/auth/session.ts';
setAuthenticated({access_token:'fixture',expires_in:3600,token_type:'Bearer'},{id:'asset-qa',username:'qa'});
localStorage.setItem('creative-egress-notice:asset-qa','egress-2026-10-1');
createRoot(document.getElementById('root')).render(React.createElement(AgentPanel,{account:'asset-qa',canvasID:'canvas',defaultOpen:true}));
</script></body></html>`)
const browser = await chromium.launch({headless:true})
try {
 const page = await browser.newPage({viewport:{width:1440,height:900}})
 const errors=[];page.on('pageerror',error=>errors.push(error.message))
 const posts=[];let ready=false
 const upload=()=>({id:'upload',state:ready?'ready':'uploading',io_phase:'none',revision:'1',kind:'image',mime:'image/png',size:68,file_name:'clipboard.png',part_size:5242880,part_count:1,uploaded_parts:[],target_kind:'staged',content_revision_id:ready?'ccrv_pasted':null})
 await page.route('**/api/v1/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname
  const json=value=>route.fulfill({json:value})
  if(req.method()==='POST')posts.push({path,body:req.postDataJSON()})
  if(path.endsWith('/agent/catalog'))return json({limits:{attachments:10,image_preview_count:8},models:[{model_key:'vision',display_name:'视觉模型',capability:{image_input:true},available:true}],skills:[]})
  if(path.endsWith('/media-capabilities'))return json({schema_version:1,formats:[{kind:'image',mime:'image/png',extensions:['.png']}],image_max_bytes:10485760,av_max_bytes:10485760,part_size:5242880,batch_limit:20})
  if(path.endsWith('/uploads'))return json(upload())
  if(path.endsWith('/part-authorizations'))return json({upload_id:'upload',parts:[{part_number:1,url:base+'/qa-storage/upload',method:'PUT',headers:{}}]})
  if(path.endsWith('/complete')){ready=true;return json(upload())}
  if(path.endsWith('/uploads/upload'))return json(upload())
  if(path.endsWith('/egress-consents'))return json({id:'consent'})
  if(path.endsWith('/runs')&&req.method()==='POST')return json({id:'run',conversation_id:'conversation',trigger_message_id:'sent',state:'succeeded',settlement_state:'settled'})
  if(path.endsWith('/agent-runs/run'))return json({id:'run',conversation_id:'conversation',trigger_message_id:'sent',state:'succeeded',settlement_state:'settled',blocks:[]})
  if(path.endsWith('/conversations'))return json({items:[{id:'conversation',title:'已有对话'}]})
  if(path.endsWith('/messages'))return json({items:[{id:'history',ordinal:'1',role:'user',status:'complete',body:{schema_version:1,blocks:[{type:'content_ref',ref_id:'ccrv_old'}]}}]})
  return json({items:[]})
 })
 await page.route('**/qa-storage/**',route=>route.fulfill({status:200,body:''}))
 await page.goto(`${base}/.asset-lookup-qa.html`)
 await page.getByLabel('创作问题',{exact:true}).waitFor()
 await page.waitForFunction(()=>performance.getEntriesByType('resource').some(e=>e.name.endsWith('/media-capabilities')))
 await page.getByLabel('创作问题',{exact:true}).evaluate(el=>{
  const data=new DataTransfer();data.items.add(new File([new Uint8Array([137,80,78,71,13,10,26,10])],'clipboard.png',{type:'image/png'}));
  el.dispatchEvent(new ClipboardEvent('paste',{bubbles:true,cancelable:true,clipboardData:data}))
 })
 await page.getByRole('list',{name:'上传的参考图'}).waitFor()
 await page.waitForFunction(()=>document.querySelector('button[aria-label="发送"]')?.disabled===false)
 assert.equal(posts.find(p=>p.path.endsWith('/uploads')).body.payload.target.kind,'staged')
 assert.equal(posts.some(p=>p.path.endsWith('/assets')),false,'pasting must not save to library')
 await page.getByRole('button',{name:'发送',exact:true}).click()
 await page.getByRole('heading',{name:'发送前说明'}).waitFor()
 assert.match(await page.getByText(/助手还会按需查找资产库/).innerText(),/按需查找资产库/)
 await page.getByRole('button',{name:'知道了，发送'}).click()
 await page.waitForFunction(()=>document.querySelectorAll('.cc-agent-uploads li').length===0)
 const consent=posts.find(p=>p.path.endsWith('/egress-consents')).body.payload
 assert.equal(consent.mode,'account_library')
 assert.deepEqual(consent.selected_revision_ids,['ccrv_pasted'],'old message references must not enter new grant')
 assert(consent.data_classes.includes('media_metadata'))
 const run=posts.find(p=>p.path.endsWith('/runs')).body.payload
 assert(run.instruction.instruction_segments.some(s=>s.content_revision_id==='ccrv_pasted'))
 assert.equal(await page.evaluate(()=>localStorage.getItem('creative-egress-notice:asset-qa')),version)
 await page.setViewportSize({width:390,height:844})
 const layout=await page.locator('.cc-agent-panel').evaluate(el=>({width:el.getBoundingClientRect().width,viewport:innerWidth,overflow:el.scrollWidth-el.clientWidth}))
 assert(layout.width<=layout.viewport&&layout.overflow<=1,JSON.stringify(layout))
 assert.deepEqual(errors,[])
 console.log('PASS: paste -> staged upload -> current-only consent -> content_ref; notice version refresh; no library insert; mobile no horizontal overflow')
} finally {await browser.close();await rm(fixture,{force:true})}

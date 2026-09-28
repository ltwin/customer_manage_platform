// Browser regression for focus and local feedback. All API traffic is mocked;
// run against the Vite dev server: node scripts/creative-canvas-interaction.e2e.mjs.
import assert from 'node:assert/strict'
import { chromium } from 'playwright'

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({
  viewport: process.env.TEST_VIEWPORT === 'mobile' ? {width:390,height:844} : {width:1600,height:1000},
})
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', (error) => errors.push(error.message))
const makeNode = (id, x) => ({
  id,
  parent_id: null,
  metadata: {
    type_key: 'core.text',
    type_version: 1,
    title: id,
    intent: '',
    x,
    y: 150,
    width: 280,
    height: 180,
    z_order: 0,
  },
  prompt: null,
  capabilities: {
    actions: [
      'move',
      'resize',
      'rename',
      'duplicate',
      'remove',
      'edit',
      'reference',
    ],
    prompt_mode: 'draft',
    disabled_reason: null,
  },
  status: {
    content_state: 'empty',
    generation_state: 'idle',
    active_execution_id: null,
    latest_execution_id: null,
    apply_state: null,
    error: null,
    status_revision: '1',
  },
  placement_revision: '1',
  data_revision: '1',
  data: {
    schema_version: 1,
    config: {},
    content_id: null,
    content_revision_id: null,
    selected_version_id: null,
    document_id: null,
  },
})
const canvas = {
  id: 'qa',
  project_id: 'qa-project',
  project_name: '交互回归',
  project_revision: '1',
  revision: '1',
  topology_revision: '1',
  archived: false,
  nodes: [makeNode('first', 100), makeNode('second', 650)],
  edges: [{
    id: 'qa-reference', source_node_id: 'first', target_node_id: 'second',
    source_port: 'output', target_port: 'reference', role: 'reference',
    ordinal: 0, revision: '1',
  }],
  node_inputs: [],
  changes: [],
  object_states: [],
}
let snapshotReads = 0
await page.addInitScript(() => {
  const original = window.fetch.bind(window)
  window.__canvasStreams = []
  window.fetch = async (input, init) => {
    if (!String(input).endsWith('/canvases/qa/events')) return original(input, init)
    const connection = { closed: false, auth: new Headers(init?.headers).get('Authorization') }
    window.__canvasStreams.push(connection)
    return new Response(new ReadableStream({
      start(controller) {
        connection.invalidate = () => controller.enqueue(new TextEncoder().encode('event: invalidate\ndata: {}\n\n'))
        connection.invalidate()
        init.signal.addEventListener('abort', () => {
          connection.closed = true
          controller.error(new DOMException('Aborted', 'AbortError'))
        }, { once: true })
      },
    }), { headers: { 'Content-Type': 'text/event-stream' } })
  }
})
let generationPosts=0, commandPosts=0, available=true
const versions=[]
const execution={execution_id:'e-1',node_id:'first',canvas_id:'qa',action_key:'gateway.generation',executor_version:'1',state:'succeeded',apply_state:'conflicted',execution_epoch:1,deadline:new Date(Date.now()+3600000).toISOString(),change_id:null,error:'',created_at:new Date().toISOString()}
await page.route('**/api/v1/**', async route=>{
 const path=new URL(route.request().url()).pathname
 const json=value=>route.fulfill({json:value})
 if(path.endsWith('/auth/refresh'))return json({access_token:'qa-token',token_type:'Bearer',expires_in:600})
 if(path.endsWith('/me'))return json({id:'g5-qa',email:'g5@example.test',created_at:'2026-01-01T00:00:00Z',timezone:'Asia/Shanghai'})
 if(path.endsWith('/generation-catalog'))return json({models:available?[{key:'qa-model',label:'契约文字模型',kind:'text',configuration_revision:'1',parameters:[{key:'count',label:'数量',type:'integer',required:true,options:[]}]}]:[],disabled_reason:available?'':'尚未配置生成模型'})
 if(path.endsWith('/versions'))return json({items:versions,selected_version_id:canvas.nodes[0].data.selected_version_id,data_revision:canvas.nodes[0].data_revision})
 if(path.endsWith('/commands')){
  commandPosts++
  const v=route.request().postDataJSON().payload,n=canvas.nodes.find(n=>n.id===v.node_id)||canvas.nodes[0]
  let response={}
  if(v.type==='save_prompt'){
   if(v.expected_draft_revision!==(n.prompt?.draft_revision??null))return route.fulfill({status:409,json:{error:{code:'creative_revision_conflict',message:'草稿冲突'}}})
   n.prompt={draft_id:'d-'+n.id,draft_revision:String(Number(n.prompt?.draft_revision??0)+1),action_key:v.action_key,text:v.text,model_key:v.model_key,parameters:v.parameters,references:v.references.map((r,i)=>({...r,id:'ref-'+i,node_id:n.id,revision:'1'}))}
   response={draft_id:n.prompt.draft_id,draft_revision:n.prompt.draft_revision,change:{}}
  }
  if(v.type==='batch')for(const a of v.actions){
   if(a.type==='select_version')n.data.selected_version_id=a.version_id
   if(a.type==='delete_version')versions.splice(versions.findIndex(x=>x.id===a.version_id),1)
  }
  canvas.revision=String(Number(canvas.revision)+1)
  return json(response)
 }
 if(path.endsWith('/executions') && route.request().method()==='POST'){
  generationPosts++
  const v=route.request().postDataJSON().payload
  assert.equal(v.configuration_revision,'1');assert.equal(v.draft_revision,canvas.nodes[0].prompt.draft_revision);assert(!('price' in v))
  canvas.nodes[0].status.latest_execution_id='e-1';canvas.nodes[0].status.generation_state='succeeded'
  versions.push({id:'v-1',node_id:'first',version_no:'1',origin_kind:'generation',content_revision_id:'output-1',execution_id_snapshot:'e-1',output_ordinal:0,generation_provenance:{},created_at:new Date().toISOString(),revision:'1',inputs:[]})
  canvas.object_states.push({id:'v-1',kind:'version',is_live:true,node_id:'first',revision:'1'})
  canvas.revision=String(Number(canvas.revision)+1)
  return json(execution)
 }
 if(path.endsWith('/executions/e-1'))return json(execution)
 if(path.endsWith('/content-revisions/output-1'))return json({id:'output-1',content_id:'content-1',kind:'text',schema_version:1,payload:{body:'已保存的作品版本'},media:[],truncated:false})
 if(path.endsWith('/canvases/qa')){snapshotReads++;return json(canvas)}
 if(path.endsWith('/library-settings'))return json({revision:'1',view_mode:'grid',sort:'updated_desc',thumbnail_size:'medium'})
 if(path.endsWith('/media-capabilities'))return json({formats:[],image_max_bytes:1000000,av_max_bytes:10000000,batch_limit:10})
 return json({items:[],next_cursor:'',total_count:0,library_revision:'1'})
})
try{
 await page.goto(`${process.env.CREATIVE_EDITOR_URL??'http://localhost:5173'}/creative/canvases/qa`)
 await page.locator('.react-flow__node[data-id="first"]').click({position:{x:40,y:30}})
 const panel=page.getByRole('complementary',{name:'节点生成'})
 await panel.waitFor()
 await panel.getByLabel('提示词',{exact:true}).fill('秋日森林里的摄影布景')
 await panel.getByLabel('生成模型',{exact:true}).selectOption('qa-model')
 await panel.getByLabel('数量',{exact:true}).fill('2')
 const promptBounds=await panel.boundingBox()
 assert(promptBounds.y+promptBounds.height<=page.viewportSize().height,'prompt extends below viewport')
 await panel.getByRole('button',{name:'保存草稿',exact:true}).click()
 await page.waitForFunction(()=>document.querySelector('.cc-generation footer')?.textContent.includes('草稿已保存'))
 assert.equal(canvas.nodes[0].prompt.parameters.count,2)
 await page.reload()
 await page.locator('.react-flow__node[data-id="first"]').click({position:{x:40,y:30}})
 await panel.waitFor()
 assert.equal(await panel.getByLabel('提示词',{exact:true}).inputValue(),'秋日森林里的摄影布景')
 await panel.getByRole('button',{name:'生成新版本',exact:true}).click()
 await page.waitForFunction(()=>document.querySelector('.cc-generation header')?.textContent.includes('生成已完成'))
 assert.equal(generationPosts,1)
 await panel.getByRole('button',{name:/作品版本/}).click()
 await panel.getByRole('button',{name:'预览',exact:true}).click()
 await panel.getByText('已保存的作品版本',{exact:true}).waitFor()
 await panel.getByRole('button',{name:'预览',exact:true}).click()
 await panel.getByText('已保存的作品版本',{exact:true}).waitFor()
 assert.equal(canvas.nodes[0].data.selected_version_id,null)
 const before=commandPosts
 await panel.getByRole('button',{name:'用于节点',exact:true}).click()
 await panel.getByText('版本 1 · 当前采用',{exact:true}).waitFor()
 assert.equal(commandPosts,before+1)
 await page.screenshot({path:'/tmp/g5-desktop.png'})
 const desktop=await panel.evaluate(el=>{const r=el.getBoundingClientRect(),s=getComputedStyle(el);return {x:r.x,y:r.y,width:r.width,height:r.height,background:s.backgroundColor,color:s.color}})
 assert(desktop.width>300 && desktop.x>=0)
 if(await page.getByRole('button',{name:'收起资产库',exact:true}).isVisible()) await page.getByRole('button',{name:'收起资产库',exact:true}).click()
 await page.setViewportSize({width:390,height:844})
 await page.screenshot({path:'/tmp/g5-mobile.png'})
 const mobile=await panel.boundingBox();assert(mobile.x>=0 && mobile.x+mobile.width<=390)
 available=false
 await page.reload()
 await page.getByRole('button',{name:'适应全部节点',exact:true}).click()
 await page.locator('.react-flow__node[data-id="first"]').click()
 await panel.getByText('尚未配置生成模型',{exact:true}).waitFor()
 assert(await panel.getByRole('button',{name:'生成新版本',exact:true}).isDisabled())
 assert(!await panel.getByRole('button',{name:'保存草稿',exact:true}).isDisabled())
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({generationPosts,commandPosts,desktop,mobile,errors}))
}catch(e){await page.screenshot({path:'/tmp/g5-browser-failure.png'});throw e}finally{await browser.close()}

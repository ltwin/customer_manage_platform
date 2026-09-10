/** 原生 select 仅保留值与 change 契约；可见选择界面统一使用玻璃列表。 */
export function createGlassSelects({ids,icon,beforeOpen}){
  const controls=new Map(),popup=document.createElement('div');popup.id='glass-select-menu';popup.className='glass-select-menu glass';popup.hidden=true;popup.setAttribute('role','listbox');document.body.append(popup);
  let active=null;
  const labels={'media-type':'素材类型',sort:'排序','tag-match-mode':'标签匹配方式',orientation:'图片方向',size:'卡片大小','trash-retention':'自动清理'};
  const symbols={all:'grid',image:'image',text:'text-lines',link:'link',video:'video',recent:'clock',oldest:'clock',name:'text-lines'};
  for(const id of ids){
    const select=document.getElementById(id),button=document.createElement('button');button.type='button';button.id=`${id}-trigger`;button.className='glass-select-trigger';button.setAttribute('aria-haspopup','listbox');button.setAttribute('aria-expanded','false');button.setAttribute('aria-controls',popup.id);select.hidden=true;select.after(button);controls.set(id,{select,button});select.addEventListener('change',sync);
    button.addEventListener('click',event=>open(id,event.detail===0));
    button.addEventListener('keydown',event=>{if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){event.preventDefault();if(active?.id===id)focusOption(event.key==='ArrowUp'||event.key==='End'?-1:0);else open(id,true);}});
  }
  function sync(){
    for(const [id,{select,button}] of controls){
      const option=[...select.options].find(option=>option.value===select.value)||select.options[0];
      button.innerHTML=`${id==='media-type'?icon(symbols[option.value]||'grid'):''}<span>${option.textContent}</span><span class="glass-select-chevron">${icon('chevron-down')}</span>`;
      button.setAttribute('aria-label',`${labels[id]||'选择'}：${option.textContent}`);button.dataset.value=option.value;
    }
    if(active)close();
  }
  function close(restore=false){
    const button=active?.button;popup.hidden=true;button?.setAttribute('aria-expanded','false');active=null;if(restore&&button?.isConnected)button.focus({preventScroll:true});
  }
  function focusOption(index){const choices=[...popup.querySelectorAll('button:not(:disabled)')];choices[index<0?choices.length-1:index]?.focus({preventScroll:true});}
  function open(id,keyboard=false){
    if(active?.id===id){close(true);return;}
    close();beforeOpen?.();const {select,button}=controls.get(id);active={id,select,button};button.setAttribute('aria-expanded','true');popup.setAttribute('aria-label',`选择${labels[id]||'选项'}`);
    popup.innerHTML=[...select.options].map(option=>{
      const [label,note]=option.textContent.split(' · '),row=document.createElement('button');row.type='button';row.tabIndex=-1;row.dataset.value=option.value;row.setAttribute('role','option');row.setAttribute('aria-selected',String(option.value===select.value));row.disabled=option.disabled;row.setAttribute('aria-disabled',String(option.disabled));
      row.innerHTML=icon(symbols[option.value]||'grid');const text=document.createElement('span');text.className='glass-select-option-text';text.textContent=label;row.append(text);
      if(note){const badge=document.createElement('small');badge.textContent=note;row.append(badge);}else{const mark=document.createElement('span');mark.className='glass-select-check';mark.innerHTML=icon('check');row.append(mark);}
      return row.outerHTML;
    }).join('');
    popup.hidden=false;const rect=button.getBoundingClientRect(),width=popup.offsetWidth,height=popup.offsetHeight,bottom=innerHeight-(innerWidth<=760?88:10);
    popup.style.left=`${Math.max(8,Math.min(id==='sort'?rect.right-width:rect.left,innerWidth-width-8))}px`;
    popup.style.top=`${Math.max(8,rect.bottom+7+height<=bottom?rect.bottom+7:Math.min(rect.top-height-7,bottom-height))}px`;
    if(keyboard)(popup.querySelector('[aria-selected="true"]:not(:disabled)')||popup.querySelector('button:not(:disabled)'))?.focus({preventScroll:true});
  }
  popup.addEventListener('click',event=>{
    const option=event.target.closest('[data-value]');if(!option||option.disabled||!active)return;const source=active.select,value=option.dataset.value;close(true);if(source.value!==value){source.value=value;source.dispatchEvent(new Event('change',{bubbles:true}));}
  });
  popup.addEventListener('keydown',event=>{
    const choices=[...popup.querySelectorAll('button:not(:disabled)')],index=choices.indexOf(document.activeElement);
    if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){event.preventDefault();const next=event.key==='Home'?0:event.key==='End'?choices.length-1:(index+(event.key==='ArrowDown'?1:-1)+choices.length)%choices.length;choices[next]?.focus({preventScroll:true});}
  });
  document.addEventListener('keydown',event=>{if(!active)return;if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close(true);}else if(event.key==='Tab')close(popup.contains(document.activeElement));},true);
  document.addEventListener('pointerdown',event=>{if(active&&!popup.contains(event.target)&&!active.button.contains(event.target))close();});
  document.addEventListener('focusin',event=>{if(active&&!popup.contains(event.target)&&!active.button.contains(event.target))close();});
  document.addEventListener('scroll',event=>{if(active&&!popup.contains(event.target))close(popup.contains(document.activeElement));},true);
  window.addEventListener('resize',()=>close(popup.contains(document.activeElement)));sync();return {sync,close};
}

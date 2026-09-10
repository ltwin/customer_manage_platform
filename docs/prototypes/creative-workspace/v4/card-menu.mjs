import {ordinaryCollection} from './collections.mjs';
/** 共享浮层避开瀑布流卡片的 transform/contain，所有菜单动作复用既有操作。 */
export function createCardMenu(ctx){
  const menu=document.getElementById('asset-menu');let opener=null,assetId=null,anchorPosition=null;
  function close(restoreFocus=false){
    const previous=opener;menu.hidden=true;opener?.setAttribute('aria-expanded','false');opener=null;assetId=null;anchorPosition=null;
    if(restoreFocus&&previous?.isConnected)previous.focus({preventScroll:true});
  }
  function open(button,last=false){
    if(button===opener){close(true);return;}
    close();const isCollection=Boolean(button.dataset.collectionMenu);let subject;
    if(isCollection){try{subject=ordinaryCollection(ctx.getState(),button.dataset.collectionMenu);}catch{return;}}
    else subject=ctx.getState().assets.find(a=>a.id===button.dataset.assetMenu);
    if(!subject)return;
    opener=button;assetId=subject.id;button.setAttribute('aria-expanded','true');
    const favorite=ctx.getState().favorites.includes(subject.id);
    const actions=isCollection?[['collection-open','layers','打开集合'],['collection-rename','edit','重命名'],['collection-delete','trash','删除集合']]:[['detail','image','查看详情'],['favorite','heart',favorite?'移出我的最爱':'加入我的最爱'],['collections','layers','加入集合'],['projects','folder',ctx.projectLabel||'加入项目'],['trash','trash','移入回收站']];
    menu.innerHTML=actions.map(([action,icon,label])=>{const danger=action==='trash'||action==='collection-delete';return `${danger?'<div role="separator" class="asset-menu-divider"></div>':''}<button role="menuitem" tabindex="-1" type="button" data-menu-action="${action}" class="${danger?'danger-text':''}">${ctx.icon(icon)}<span>${label}</span></button>`;}).join('');
    menu.setAttribute('aria-label',`${isCollection?subject.name:subject.title}的快捷操作`);menu.hidden=false;
    const rect=button.getBoundingClientRect(),width=menu.offsetWidth,height=menu.offsetHeight;anchorPosition={top:rect.top,left:rect.left};
    const floor=innerWidth<=760?90:12,maxBottom=innerHeight-floor;
    const left=Math.max(8,Math.min(rect.right-width,innerWidth-width-8));
    const top=rect.bottom+height+7<=maxBottom?rect.bottom+7:Math.max(8,Math.min(rect.top-height-7,maxBottom-height));
    menu.style.left=`${left}px`;menu.style.top=`${top}px`;
    const controls=menu.querySelectorAll('button');controls[last?controls.length-1:0].focus({preventScroll:true});
  }
  for(const id of ['gallery','group-grid']){
    document.getElementById(id).addEventListener('click',event=>{const button=event.target.closest('[data-asset-menu],[data-collection-menu]');if(button)open(button);});
    document.getElementById(id).addEventListener('keydown',event=>{
      const button=event.target.closest('[data-asset-menu],[data-collection-menu]');if(button&&['ArrowDown','ArrowUp'].includes(event.key)){event.preventDefault();open(button,event.key==='ArrowUp');}
    });
  }
  menu.addEventListener('click',event=>{
    const button=event.target.closest('[data-menu-action]');if(!button)return;
    const id=assetId,trigger=opener;close(true);ctx.run(button.dataset.menuAction,id,trigger);
  });
  menu.addEventListener('keydown',event=>{
    const controls=[...menu.querySelectorAll('button')],index=controls.indexOf(document.activeElement);
    if(['ArrowDown','ArrowUp','Home','End'].includes(event.key)){
      event.preventDefault();const next=event.key==='Home'?0:event.key==='End'?controls.length-1:(index+(event.key==='ArrowDown'?1:-1)+controls.length)%controls.length;controls[next].focus();
    }else if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close(true);}
    else if(event.key==='Tab')close(true);
  });
  document.addEventListener('pointerdown',event=>{if(opener&&!menu.contains(event.target)&&!opener.contains(event.target))close();});
  document.addEventListener('focusin',event=>{if(opener&&!menu.contains(event.target)&&!opener.contains(event.target))close();});
  document.addEventListener('scroll',event=>{if(opener&&!menu.contains(event.target)){const rect=opener.getBoundingClientRect();if(Math.abs(rect.top-anchorPosition.top)>.5||Math.abs(rect.left-anchorPosition.left)>.5)close(menu.contains(document.activeElement));}},true);
  window.addEventListener('resize',()=>close(menu.contains(document.activeElement)));
  return {close};
}

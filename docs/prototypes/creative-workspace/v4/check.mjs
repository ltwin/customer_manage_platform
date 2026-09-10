import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import { pack } from './masonry.mjs';
import { FAVORITES, UNFILED, collectionViews, deleteCollection, renameCollection, removeMembers } from './collections.mjs';
import { projectDocument, snapshotShot } from './projects.mjs';

const photos=JSON.parse(readFileSync(new URL('./photos.json',import.meta.url),'utf8'));
for(const photo of photos){
  assert(photo.width>0&&photo.height>0);
  assert(existsSync(new URL(photo.src,import.meta.url)),photo.src);
}
for(const width of [284,339,354,550,950,1250,1650]) {
  const items=[...photos,{width:4,height:4},{width:4,height:7}];
  const result=pack(items,width,{mobile:width<600});
  result.positions.forEach((a,i)=>{
    assert(a.width>0); assert(a.x>=0&&a.x+a.width<=width+.01);
    assert(Math.abs((a.height-(width<600?57:62))/a.width-items[i].height/items[i].width)<.000001);
    for(const b of result.positions.slice(i+1)) {
      const xOverlap=Math.min(a.x+a.width,b.x+b.width)-Math.max(a.x,b.x)>.01;
      const yOverlap=Math.min(a.y+a.height,b.y+b.height)-Math.max(a.y,b.y)>.01;
      assert(!(xOverlap&&yOverlap),'masonry cards overlap');
    }
  });
}
assert.equal(pack([],800).height,0);
console.log('PASS: image manifests, seven responsive widths, original aspect ratios, no overlaps, empty layout');

const state={assets:[{id:'a',src:'a.jpg'},{id:'b',src:'b.jpg'},{id:'c',src:'c.jpg'}],favorites:['a'],projects:[{id:'p',items:['b']}],collections:[]};
assert.deepEqual(collectionViews(state).find(g=>g.id===UNFILED).items,['a','b','c'],'favorites and project use do not classify');
state.collections=[{id:'one',name:'一',items:['a','b']},{id:'two',name:'二',items:['b']}];
const before=structuredClone(state),removed=deleteCollection(state,'one');
assert.deepEqual(state,before,'helpers do not mutate persisted source state');
assert.deepEqual(removed.assets,state.assets);assert.deepEqual(removed.favorites,state.favorites);assert.deepEqual(removed.projects,state.projects);
assert.deepEqual(collectionViews(removed).find(g=>g.id===UNFILED).items,['a','c'],'other collection still classifies b');
assert.deepEqual(collectionViews(removeMembers(removed,'two',['b'])).find(g=>g.id===UNFILED).items,['a','b','c']);
assert.equal(renameCollection(state,'one',' 新名称 ').collections[0].name,'新名称');
for(const id of [FAVORITES,UNFILED]){assert.throws(()=>deleteCollection(state,id));assert.throws(()=>renameCollection(state,id,'改名'));}
assert.throws(()=>removeMembers(state,UNFILED,['a']));
assert.deepEqual(removeMembers(state,FAVORITES,['a']).favorites,[]);
console.log('PASS: immutable system collections, independent favorites/projects, multi-collection classification, rename/delete/remove preserve assets');

const reference={id:'ref',kind:'image',src:'original.jpg',width:600,height:900,title:'原始参考'};
const captured=snapshotShot(reference,'shot');reference.src='changed.jpg';reference.title='更新后的参考';
assert.equal(captured.image,'original.jpg');assert.equal(captured.sourceTitle,'原始参考');
const blank=projectDocument({id:'new-project',items:['ref']},[reference]);assert.equal(blank.brief,'');assert.deepEqual(blank.shots,[]);
const existing={brief:'摄影师手写内容',shots:[captured]};assert.equal(projectDocument({id:'p-sea',items:[],document:existing},[]),existing);
assert.equal(projectDocument({id:'p-sea',items:['ref']},[reference]).shots.length,1);
console.log('PASS: manual projects stay blank, existing content is preserved, shot references are snapshots');

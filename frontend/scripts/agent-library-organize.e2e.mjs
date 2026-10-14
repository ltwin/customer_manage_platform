// Actual component behavior: code points, preserved invalid input and legacy text.
import assert from 'node:assert/strict'
import { writeFile, rm } from 'node:fs/promises'
import { chromium } from 'playwright'
const fixture = new URL('../.library-description-qa.html', import.meta.url)
await writeFile(fixture, `<html><body><div id="root"></div><button id="outside">保存</button><script type="module">
import React from 'react';import {createRoot} from 'react-dom/client';
import Input from '/src/creative-canvas/assets/AssetDescriptionInput.tsx';
window.saved=[];const original=location.search.includes('legacy')?'长'.repeat(2000):'';
createRoot(document.getElementById('root')).render(React.createElement(Input,{description:original,disabled:false,onSave:(v)=>window.saved.push(v)}));
</script></body></html>`)
const browser=await chromium.launch({headless:true})
try{
 const page=await browser.newPage()
 const url=`${process.env.CREATIVE_AGENT_URL||'http://localhost:5179'}/.library-description-qa.html`
 await page.goto(url)
 const field=page.getByRole('textbox',{name:'备注'})
 await field.fill('🙂'.repeat(1000));
 await page.waitForFunction(()=>document.querySelector('small')?.textContent.startsWith('1000 字'))
 await page.locator('#outside').click()
 assert.equal(await page.evaluate(()=>window.saved.length),1)
 await field.fill('🙂'.repeat(1001));await page.locator('#outside').click()
 assert.equal(await field.inputValue(),'🙂'.repeat(1001))
 assert.equal(await page.evaluate(()=>window.saved.length),1)
 assert.match(await page.getByRole('alert').textContent(),/当前 1001 字/)
 await page.goto(url+'?legacy=1');await field.focus();await page.locator('#outside').click()
 assert.equal(await field.inputValue(),'长'.repeat(2000));assert.equal(await page.evaluate(()=>window.saved.length),0)
 await field.fill('新'.repeat(1001));await page.locator('#outside').click();assert.equal(await page.evaluate(()=>window.saved.length),0)
 await field.fill('新描述');await page.locator('#outside').click();assert.deepEqual(await page.evaluate(()=>window.saved),['新描述'])
 console.log('PASS: 1000 emoji saved, 1001 rejected without truncation, legacy 2000 preserved and untouched description omitted')
}finally{await browser.close();await rm(fixture,{force:true})}

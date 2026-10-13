import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? 'playwright');
const dir=fileURLToPath(new URL('../../../tasks/assets/', import.meta.url));
const origin=process.env.TRANSIT_URL ?? 'http://127.0.0.1:3000';
const b=await chromium.launch({channel:'chromium',headless:true});
const c=await b.newContext({viewport:{width:1440,height:900},timezoneId:'America/Sao_Paulo',colorScheme:'dark'});
const p=await c.newPage();
// Inspect the props supplied to the actual rendered deck.gl layer through React's current fiber.
// No provider data or product behavior is changed.
function layerProps(){
 const main=document.querySelector('main');let node=main[Object.keys(main).find(k=>k.startsWith('__reactFiber$'))];while(node.return)node=node.return;node=node.stateNode.current;
 const visit=n=>{if(!n)return null;const props=n.memoizedProps;if(props&&Array.isArray(props.vehicles)&&Object.hasOwn(props,'trails')&&props.scheme)return props;return visit(n.child)||visit(n.sibling);};
 return visit(node);
}
async function load(query){await p.goto(origin+'/'+query);await p.getByRole('slider').waitFor({timeout:60000});await p.waitForFunction(()=>document.querySelector('#deckgl-overlay')?.width>0);await p.waitForTimeout(600);}
const findings={};
try {
 await load('?t=2026-10-12T15:00Z');
 const gap=await p.evaluate((src)=>{const props=eval('('+src+')')();if(!props?.trails)throw new Error('No actual rendered trails');const w=props.trails.window;let paths=0;for(let j=0;j<w.pathSeries.length;j++){const start=w.pathStarts[j];const end=w.pathStarts[j+1];for(let k=start+1;k<end;k++)if(w.t[k]-w.t[k-1]>300)throw new Error('Rendered trail bridges a gap');paths++;}
 for(let j=1;j<w.pathSeries.length;j++){if(w.pathSeries[j]!==w.pathSeries[j-1])continue;const k=w.pathStarts[j];if(w.t[k]-w.t[k-1]<600)continue;const series=w.series[w.pathSeries[j]];const a=w.originMs+w.t[k-1]*1000;const z=w.originMs+w.t[k]*1000;return {...series,previousFix:new Date(a).toISOString(),nextFix:new Date(z).toISOString(),gapSeconds:(z-a)/1000,pathCount:paths,minuteBefore:new Date(Math.floor(a/60000)*60000).toISOString().slice(0,16)+'Z',minuteDuring:new Date(Math.floor((a+z)/2/60000)*60000).toISOString().slice(0,16)+'Z',minuteAfter:new Date(Math.floor(z/60000)*60000).toISOString().slice(0,16)+'Z'};}throw new Error('No recorded gap >=600 s');},layerProps.toString());
 console.log('REAL GAP',gap);findings.gap=gap;
 for(const [name,minute,expected] of [['before',gap.minuteBefore,true],['during',gap.minuteDuring,false],['after',gap.minuteAfter,true]]){
  await load('?onibus='+encodeURIComponent(gap.vehicle)+'&t='+encodeURIComponent(minute));
  const state=await p.evaluate(({src,vehicle,operator})=>{const props=eval('('+src+')')();return {visible:props.vehicles.some(v=>v.vehicle===vehicle&&v.operator===operator),selected:props.selected?.vehicle??null,trails:props.trails!==null, sign:document.querySelector('h1').innerText, url:location.href, sample:props.vehicles.find(v=>v.vehicle===vehicle&&v.operator===operator)};},{src:layerProps.toString(),vehicle:gap.vehicle,operator:gap.operator});
  console.log('GAP STATE',name,state);assert.equal(state.visible,expected);findings[name]=state;await p.screenshot({path:`${dir}/time-machine-gap-${name}.png`});
 }
 await p.getByRole('switch',{name:'Só posições reais'}).click();
 const real=await p.evaluate(src=>{const props=eval('('+src+')')();return {trails:props.trails!==null,interpolated:props.vehicles.filter(v=>v.interpolated).length,vehicles:props.vehicles.length};},layerProps.toString());
 assert.equal(real.trails,false);assert.equal(real.interpolated,0);findings.realPositions=real;
 console.log('REAL POSITIONS',real);
 await c.close();
 const reduced=await b.newContext({viewport:{width:1440,height:900},timezoneId:'America/Sao_Paulo',reducedMotion:'reduce'});const rp=await reduced.newPage();await rp.goto(origin+'/?t=2026-10-12T15:00Z');await rp.getByRole('slider').waitFor();await rp.waitForFunction(()=>document.querySelector('#deckgl-overlay')?.width>0);await rp.waitForTimeout(300);
 const low=await rp.evaluate(src=>{const props=eval('('+src+')')();return {trails:props.trails!==null,interpolated:props.vehicles.filter(v=>v.interpolated).length};},layerProps.toString());assert.equal(low.trails,false);assert.equal(low.interpolated,0);findings.reduced=low;
 await reduced.close();
 // Actual error/loading states: intercept only history requests, preserving all provider positions.
 for(const name of ['loading','unavailable']) {
  const ctx=await b.newContext({viewport:{width:1440,height:900},timezoneId:'America/Sao_Paulo'});
  const ep=await ctx.newPage();await ep.route('**/api/v1/history**',async route=>{if(name==='loading'){await new Promise(resolve=>setTimeout(resolve,12000));await route.abort().catch(()=>{});}else await route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'validation: history unavailable'})});});
  await ep.goto(origin+'/?t=2026-10-12T15:00Z');await ep.waitForFunction(()=>document.querySelector('#deckgl-overlay')?.width>0);
  if(name==='unavailable')await ep.getByRole('heading',{level:1}).filter({hasText:'SEM HISTÓRICO'}).waitFor({timeout:20000});
  else await ep.getByRole('heading',{level:1}).filter({hasText:'CARREGANDO'}).waitFor();
  const state=await ep.evaluate(src=>{const props=eval('('+src+')')();return {drawn:props.vehicles.length,trails:props.trails!==null,sign:document.querySelector('h1').innerText,status:[...document.querySelectorAll('[role="status"]')].map(x=>x.innerText)};},layerProps.toString());assert.equal(state.drawn,0);assert.match(state.sign,/REPLAY/);assert.ok(state.status.some(s=>s.includes('Reprodução')));findings[name]=state;await ep.screenshot({path:`${dir}/time-machine-${name}.png`});await ctx.close();
 }
 console.log('HONESTY PASS');
}catch(e){findings.failure=String(e.stack);console.error(e);process.exitCode=1;}finally{await fs.writeFile(`${dir}/time-machine-honesty-results.json`,JSON.stringify(findings,null,2)+'\n');await b.close();}

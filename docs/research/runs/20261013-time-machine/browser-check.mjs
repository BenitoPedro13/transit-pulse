import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? 'playwright');
const origin=process.env.TRANSIT_URL??'http://127.0.0.1:3000';
const out=fileURLToPath(new URL('../../../tasks/assets/', import.meta.url));
const result={screens:[],errors:[],accessibility:[],keyboard:[],mobile:[],performance:[]};
const browser=await chromium.launch({channel:'chromium',headless:true});
const context=await browser.newContext({viewport:{width:1440,height:900},colorScheme:'dark',timezoneId:'America/Sao_Paulo'});
const p=await context.newPage();
p.on('pageerror',e=>result.errors.push(e.message));
const pause=ms=>p.waitForTimeout(ms);
async function ready(page=p){await page.getByRole('slider').waitFor({timeout:60000});await page.waitForFunction(()=>document.querySelector('#deckgl-overlay')?.width>0);}
async function axeCheck(name,page=p){await page.addScriptTag({path:process.env.AXE_SCRIPT ?? fileURLToPath(import.meta.resolve('axe-core/axe.min.js'))});const v=await page.evaluate(async()=>(await axe.run(document.querySelector('main'),{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))})));result.accessibility.push({name,violations:v});console.log('AXE',name,JSON.stringify(v));}
async function shot(name){await pause(500);await p.screenshot({path:`${out}/time-machine-${name}.png`});result.screens.push(name);}
try {
 await fs.mkdir(out,{recursive:true});await p.goto(origin+'/?t=2026-10-12T15:00Z');await ready();await pause(1000);
 const gpu=await p.evaluate(()=>{const c=document.createElement('canvas');const gl=c.getContext('webgl2');const ext=gl.getExtension('WEBGL_debug_renderer_info');return gl.getParameter(ext.UNMASKED_RENDERER_WEBGL);});result.gpu=gpu;console.log('GPU',gpu);
 const slider=p.getByRole('slider');
 for(const [key,expected] of [['Home',0],['ArrowRight',1],['ArrowLeft',0],['PageUp',10],['PageDown',0],['End',117]]){await slider.press(key,{noWaitAfter:true});await pause(150);const value=Number(await slider.getAttribute('aria-valuenow'));assert.equal(value,expected);result.keyboard.push({key,value});}
 assert.equal(await p.getByRole('button',{name:'Ao vivo',exact:true}).isDisabled(),true);
 await slider.press('Home',{noWaitAfter:true});await p.locator('[aria-label="Velocidade 60 vezes"]').click();
 await p.getByRole('button',{name:'Reproduzir',exact:true}).focus();await p.keyboard.press('Space');await p.getByRole('button',{name:'Pausar',exact:true}).waitFor();await pause(300);await p.keyboard.press('Enter');await p.getByRole('button',{name:'Reproduzir',exact:true}).waitFor();
 result.keyboard.push({spaceStarts:true,enterPauses:true});
 console.log('KEYBOARD PASS');
 for(const scheme of ['dark','light']) {
  await p.emulateMedia({colorScheme:scheme});await pause(1000);
  for(const viewport of [{width:1440,height:900},{width:390,height:844}]) {
   await p.setViewportSize(viewport);await pause(600);
   if(viewport.width===390){await p.getByRole('button',{name:'Expandir painel'}).click();await pause(200);}
   await slider.press('Home',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});
   assert.match(await p.locator('h1').innerText(),/REPLAY/);
   assert.match(await p.locator('section[aria-label="Linha do tempo"]').innerText(),/Movimento suavizado/);
   await axeCheck(`${viewport.width}-${scheme}-paused`);
   await shot(`${viewport.width}x${viewport.height}-${scheme}-paused`);
   await p.getByRole('button',{name:'Reproduzir',exact:true}).click();await pause(1500);
   await shot(`${viewport.width}x${viewport.height}-${scheme}-playing`);
   await p.getByRole('button',{name:'Pausar',exact:true}).click();
   if(viewport.width===390){
    await p.getByRole('button',{name:'Recolher painel'}).click();assert.equal(await p.locator('section[aria-label="Painel"]').getAttribute('data-expanded'),'false');
    assert.match(await p.locator('section[aria-label="Linha do tempo"]').innerText(),/Movimento suavizado/);
    await shot(`390x844-${scheme}-compact`);
    await p.getByRole('tab',{name:'Linhas',exact:true}).click();
    assert.equal(await p.locator('section[aria-label="Painel"]').getAttribute('data-expanded'),'true');
    const list=p.locator('#painel-conteudo');await list.evaluate(e=>e.scrollTop=e.scrollHeight);await pause(100);
    await p.getByRole('button',{name:'Recolher painel'}).click();assert.equal(await p.locator('section[aria-label="Painel"]').getAttribute('data-expanded'),'false');
    await p.getByRole('button',{name:'Expandir painel'}).click();await p.getByRole('searchbox',{name:'Buscar linha'}).fill('483');
    await p.locator('section[aria-label="Ônibus"] button').filter({hasText:'483'}).click();await pause(500);
    await axeCheck(`390-${scheme}-selected-line`);
    await shot(`390x844-${scheme}-linhas`);
    await p.getByRole('button',{name:'Recolher painel'}).click();assert.equal(await p.locator('section[aria-label="Painel"]').getAttribute('data-expanded'),'false');
    result.mobile.push({scheme,collapseAfterScroll:true,collapseAfterSearchAndSelect:true,overflow:await p.evaluate(()=>document.documentElement.scrollWidth>innerWidth)});
    await p.getByRole('button',{name:'Expandir painel'}).click();await p.getByRole('button',{name:'Limpar linha'}).click();await p.getByRole('searchbox',{name:'Buscar linha'}).fill('');await p.getByRole('tab',{name:'Agora',exact:true}).click();await p.getByRole('button',{name:'Recolher painel'}).click();
   }
  }
 }
 console.log('LAYOUT PASS',result.mobile);
 await p.setViewportSize({width:1440,height:900});await p.emulateMedia({colorScheme:'dark'});await pause(500);
 await slider.press('Home',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});
 await p.getByRole('switch',{name:'Só posições reais'}).click();assert.equal(await p.getByRole('switch').getAttribute('aria-checked'),'true');
 await shot('real-positions');await p.getByRole('switch',{name:'Só posições reais'}).click();
 // Fresh context ensures reduced motion starts with the reader's default (no explicit smoothing override).
 const reduce=await browser.newContext({viewport:{width:1440,height:900},reducedMotion:'reduce',timezoneId:'America/Sao_Paulo'});
 const rp=await reduce.newPage();await rp.goto(origin+'/?t=2026-10-12T15:00Z');await ready(rp);await rp.waitForTimeout(500);
 assert.equal(await rp.getByRole('switch',{name:'Só posições reais'}).getAttribute('aria-checked'),'true');assert.equal(await rp.locator('[aria-label="Velocidade 60 vezes"]').isDisabled(),true);assert.equal(await rp.getByRole('button',{name:'Reproduzir'}).count(),1);
 const before=Number(await rp.getByRole('slider').getAttribute('aria-valuenow'));await rp.getByRole('button',{name:'Reproduzir'}).click();await rp.waitForTimeout(2100);await rp.getByRole('button',{name:'Pausar'}).click();const after=Number(await rp.getByRole('slider').getAttribute('aria-valuenow'));assert.ok(after-before>=2&&after-before<=3);result.reduced={noAutoplay:true,realPositionsDefault:true,speedDisabled:true,minutesIn2100ms:after-before};await rp.screenshot({path:`${out}/time-machine-reduced-motion.png`});await reduce.close();
 console.log('REDUCED PASS');
 // Separate real-time samples. requestAnimationFrame measures browser frame scheduling while the actual map and trails run.
 await p.locator('[aria-label="Velocidade 60 vezes"]').click();
 for(let run=0;run<3;run++){
  await slider.press('Home',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});await p.getByRole('button',{name:'Reproduzir'}).click();
  const sample=await p.evaluate(()=>new Promise(resolve=>{let frames=0;const start=performance.now();function tick(now){frames++;if(now-start>=10000)resolve({frames,ms:now-start,fps:frames*1000/(now-start)});else requestAnimationFrame(tick);}requestAnimationFrame(tick);}));
  await p.getByRole('button',{name:'Pausar'}).click();result.performance.push(sample);console.log('FPS',run,sample);
 }
 const fpsMedian=result.performance.map(x=>x.fps).sort((a,b)=>a-b)[1];
 assert.ok(fpsMedian>=30,`Median frame rate ${fpsMedian} is below 30 fps`);
 const seekTimes=[];
 for(let run=0;run<3;run++) {await slider.press('Home',{noWaitAfter:true});const t=performance.now();await slider.press('PageUp',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});await slider.press('PageUp',{noWaitAfter:true});await p.waitForFunction(()=>document.querySelector('h1').innerText.includes('12:07'));seekTimes.push(performance.now()-t);}result.cachedSeekMs=seekTimes;console.log('SEEK',seekTimes);
 await slider.press('Home',{noWaitAfter:true});const start=performance.now();await p.getByRole('button',{name:'Reproduzir'}).click();console.log('PLAYBACK_START');await p.getByRole('button',{name:'Ao vivo',exact:true}).waitFor({timeout:150000});result.playbackSeconds=(performance.now()-start)/1000;console.log('PLAYBACK_END',result.playbackSeconds);assert.ok(result.playbackSeconds>=112&&result.playbackSeconds<=123);
 assert.ok(seekTimes.every(ms=>ms<1000),'A cached cross-hour seek took at least one second');
 assert.equal(result.errors.length,0);assert.ok(result.accessibility.every(x=>x.violations.length===0));
 console.log('PASS');
} catch(error) {result.failure=String(error.stack);console.error(error);process.exitCode=1;} finally {await fs.writeFile(`${out}/time-machine-browser-results.json`,JSON.stringify(result,null,2)+'\n');await browser.close();}

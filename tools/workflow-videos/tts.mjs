import fs from 'node:fs/promises';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
process.loadEnvFile(path.join(root,'.env'));
const key=process.env.OPENROUTER_API_KEY;
if(!key) throw new Error('Set OPENROUTER_API_KEY in the private root .env.');
const model=process.env.OPENROUTER_TTS_MODEL||'google/gemini-3.8-flash-lite-tts';
const voice=process.env.OPENROUTER_TTS_VOICE||'Kore';
const format=process.env.OPENROUTER_TTS_FORMAT||'pcm';
if(!['mp3','pcm','wav'].includes(format)) throw new Error('Unsupported speech format');
const run=path.resolve(process.argv.slice(2).find(x=>!x.startsWith('--'))||path.join(root,'output/playwright/workflow-videos-2026-09-26'));
await fs.mkdir(path.join(run,'audio'),{recursive:true});
const catalog=await fetch('https://openrouter.ai/api/v1/models?output_modalities=speech').then(r=>r.json());
const entry=catalog.data.find(x=>x.id===model);
if(!entry?.architecture?.output_modalities?.includes('speech')) throw new Error('Configured model is not a speech model.');
if(entry.supported_voices?.length&&!entry.supported_voices.includes(voice)) throw new Error('Configured voice is not supported by selected model.');
await fs.writeFile(path.join(run,'tts-model.json'),JSON.stringify(entry,null,2));
const scenes=process.argv.includes('--sample')?[{id:'voice-sample',narration:'Welcome to Fervid Budget. In these walkthroughs, we will follow a request from submission to approval, record a payment, and check the remaining balance. All records shown are demonstration data.'}]:JSON.parse(await fs.readFile(path.join(run,'storyboard.json'),'utf8')).videos.flatMap(v=>v.scenes);
const manifest=[];
async function generate(scene){
 const fingerprint=crypto.createHash('sha256').update(JSON.stringify({model,voice,format,text:scene.narration})).digest('hex');
 const output=path.join(run,'audio',scene.id+(format==='pcm'?'.wav':'.'+format)),meta=output+'.json';
 let saved;try{saved=JSON.parse(await fs.readFile(meta,'utf8'));}catch{}
 if(saved?.fingerprint===fingerprint){try{await fs.access(output);manifest.push(saved);console.log(scene.id+': cached');return;}catch{}}
 const res=await fetch('https://openrouter.ai/api/v1/audio/speech',{method:'POST',redirect:'error',headers:{Authorization:`Bearer ${key}`,'Content-Type':'application/json','X-OpenRouter-Title':'Fervid Budget workflow videos'},body:JSON.stringify({model,input:scene.narration,voice,response_format:format}),signal:AbortSignal.timeout(120000)});
 if(!res.ok){let message='';try{const j=await res.json();message=String(j.error?.message||j.message||'');}catch{};throw new Error(`Speech request failed (${res.status}): ${message.replaceAll(key,'[REDACTED]').slice(0,300)}`);}
 const bytes=Buffer.from(await res.arrayBuffer());if(bytes.length<1000) throw new Error('Speech response is unexpectedly short.');
 if((res.headers.get('content-type')||'').includes('json'))throw new Error('Unexpected JSON speech response.');
 if(format==='pcm' && bytes.subarray(0,4).toString()!=='RIFF'){
 const raw=output+'.pcm';await fs.writeFile(raw,bytes);execFileSync('ffmpeg',['-hide_banner','-loglevel','error','-y','-f','s16le','-ar','24000','-ac','1','-i',raw,output]);await fs.unlink(raw);
 }else{await fs.writeFile(output,bytes);}
 const probe=JSON.parse(execFileSync('ffprobe',['-v','quiet','-show_format','-show_streams','-of','json',output],{encoding:'utf8'}));
 const duration=Number(probe.format.duration);if(!(duration>0)||!probe.streams.some(s=>s.codec_type==='audio'))throw new Error('Generated file is not playable audio');
 saved={id:scene.id,model,voice,format,fingerprint,duration,bytes:bytes.length,generationId:res.headers.get('x-generation-id'),file:path.relative(run,output),createdAt:new Date().toISOString()};
 await fs.writeFile(meta,JSON.stringify(saved,null,2));manifest.push(saved);console.log(`${scene.id}: ${duration.toFixed(1)}s audio saved`);
}
let next=0;
await Promise.all(Array.from({length:Math.min(3,scenes.length)},async()=>{while(next<scenes.length){const scene=scenes[next++];await generate(scene);}}));
manifest.sort((a,b)=>a.id.localeCompare(b.id));
await fs.writeFile(path.join(run,process.argv.includes('--sample')?'voice-sample.json':'audio-manifest.json'),JSON.stringify(manifest,null,2));

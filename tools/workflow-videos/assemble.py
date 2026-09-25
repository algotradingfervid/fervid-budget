"""Compose real recordings, Remotion frames and speech. No libass dependency."""
import argparse, pathlib, json, subprocess, math, hashlib, os
from PIL import Image, ImageDraw, ImageFont
ROOT=pathlib.Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('run',nargs='?',default=str(ROOT/'output/playwright/workflow-videos-2026-09-26'));p.add_argument('--video',action='append',default=[]);p.add_argument('--ready-only',action='store_true');a=p.parse_args();RUN=pathlib.Path(a.run).resolve()
FFMPEG=os.environ.get('FFMPEG','ffmpeg');FPS=30
for folder in ['renders','videos','assets']:(RUN/folder).mkdir(parents=True,exist_ok=True)
def run(args):subprocess.run([FFMPEG,'-hide_banner','-loglevel','error','-y',*map(str,args)],check=True)
def probe(path):return json.loads(subprocess.check_output(['ffprobe','-v','quiet','-show_format','-show_streams','-of','json',str(path)],text=True))
def digest(path):
 h=hashlib.sha256()
 with open(path,'rb') as f:
  for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
 return h.hexdigest()
def fingerprint(data):return hashlib.sha256(json.dumps(data,sort_keys=True).encode()).hexdigest()
def cached(out,key):return out.exists() and out.with_suffix(out.suffix+'.sha256').exists() and out.with_suffix(out.suffix+'.sha256').read_text()==key
def mark(out,key):out.with_suffix(out.suffix+'.sha256').write_text(key)
def stamp(t):
 ms=round(t*1000);return f'{ms//3600000:02}:{ms//60000%60:02}:{ms//1000%60:02},{ms%1000:03}'
def captions(text,duration):
 # Phrase timing follows word count within each generated audio scene, not word-level alignment.
 chunks=[];chunk=[]
 for w in text.split():
  if chunk and len(' '.join(chunk+[w]))>94:chunks.append(' '.join(chunk));chunk=[]
  chunk.append(w)
  if len(chunk)>7 and w[-1:] in '.?!;':chunks.append(' '.join(chunk));chunk=[]
 if chunk:chunks.append(' '.join(chunk))
 total=sum(len(c.split()) for c in chunks);elapsed=.2;out=[]
 for c in chunks:
  end=elapsed+(duration-.4)*len(c.split())/total;out.append((elapsed,end,c));elapsed=end
 return out
FONT=next((x for x in ['/System/Library/Fonts/Supplemental/Arial.ttf','/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf'] if pathlib.Path(x).exists()),None)
if not FONT:raise RuntimeError('A local Arial or DejaVu Sans font is required for caption rendering.')
def caption_video(scene,caps,duration):
 out=RUN/'assets'/f'{scene}-captions.mp4';key=fingerprint(['caption-strip-v2',caps,duration,FONT])
 if cached(out,key):return out
 folder=RUN/'assets'/f'{scene}-captions';folder.mkdir(exist_ok=True);entries=[];cursor=0
 def add(text,length):
  if length<=0:return
  img=Image.new('RGB',(1600,62),'#25221e');d=ImageDraw.Draw(img);size=30;font=ImageFont.truetype(FONT,size)
  while d.textlength(text,font=font)>1540:size-=1;font=ImageFont.truetype(FONT,size)
  if size<24:raise RuntimeError(f'Caption too long for readable type: {text}')
  d.text((800,31),text,font=font,fill='white',anchor='mm');file=folder/f'{len(entries):03}.png';img.save(file);entries.append((file,length))
 for start,end,text in caps:
  add('',start-cursor);add(text,end-start);cursor=end
 add('',duration-cursor)
 listing=folder/'frames.ffconcat';listing.write_text('ffconcat version 1.0\n'+''.join(f"file '{file.name}'\nduration {seconds:.6f}\n" for file,seconds in entries)+f"file '{entries[-1][0].name}'\n")
 run(['-f','concat','-safe','0','-i',listing,'-t',duration,'-r',FPS,'-c:v','libx264','-threads','2','-preset','veryfast','-crf','16','-pix_fmt','yuv420p','-an',out]);mark(out,key);return out
plan=json.loads((RUN/'storyboard.json').read_text());old=RUN/'video-manifest.json';manifest={m['id']:m for m in json.loads(old.read_text())} if old.exists() else {}
wanted={x for value in a.video for x in value.split(',')}
for v in plan['videos']:
 if wanted and v['id'] not in wanted:continue
 needed=[RUN/'assets'/f'{v["id"]}-intro.mp4']
 for s in v['scenes']:needed += [RUN/'audio'/f'{s["id"]}.wav.json',RUN/'assets'/f'{s["id"]}.png',RUN/s['recording']]
 missing=[str(x.relative_to(RUN)) for x in needed if not x.exists()]
 if missing:
  if a.ready_only:print(f'Skipped {v["id"]}: {len(missing)} assets pending',flush=True);continue
  raise RuntimeError('Missing assets: '+', '.join(missing))
 if not v['scenes']:continue
 rendered=[];chapter=[];offset=3;vtts=[]
 for s in v['scenes']:
  meta=json.loads((RUN/'audio'/f'{s["id"]}.wav.json').read_text());audio=RUN/meta['file'];raw=RUN/s['recording'];frame=RUN/'assets'/f'{s["id"]}.png';out=RUN/'renders'/f'{s["id"]}.mp4'
  duration=math.ceil(max(meta['duration']+1,s['rawDuration']+.4)*FPS)/FPS
  caps=captions(s['narration'],meta['duration']);srt=RUN/'assets'/f'{s["id"]}.srt';srt.write_text('\n\n'.join(f'{i+1}\n{stamp(b)} --> {stamp(e)}\n{t}' for i,(b,e,t) in enumerate(caps))+'\n')
  capvideo=caption_video(s['id'],caps,duration)
  viewport=s.get('viewport')
  if viewport and int(viewport['width'])<1600:
   # Playwright records the narrow viewport at 0,0 within its padded 1600x900 canvas.
   # Crop only the captured page, retain its aspect, and center it in the existing slot.
   vw,vh=int(viewport['width']),int(viewport['height'])
   if not (0<vw<=1600 and 0<vh<=900):raise ValueError(f'Unsupported captured viewport: {viewport}')
   display_width=round((vw*900/vh)/2)*2
   if display_width>1600:raise ValueError(f'Viewport does not fit video slot: {viewport}')
   left=160+(1600-display_width)//2
   vf=(f'[1:v]crop={vw}:{vh}:0:0,scale={display_width}:900:flags=lanczos[phone];'
       f'[0:v]drawbox=x={left-2}:y=88:w={display_width+4}:h=904:color=0x817569:t=2[frame];'
       f'[frame][phone]overlay={left}:90:eof_action=repeat[base];'
       '[base][3:v]overlay=160:1000:eof_action=repeat,format=yuv420p[v]')
  else:
   vf='[0:v][1:v]overlay=160:90:eof_action=repeat[base];[base][3:v]overlay=160:1000:eof_action=repeat,format=yuv420p[v]'
  key=fingerprint(['assemble-v4',digest(raw),digest(frame),digest(audio),digest(capvideo),duration,vf])
  if not cached(out,key):
   run(['-loop','1','-framerate',FPS,'-i',frame,'-i',raw,'-i',audio,'-i',capvideo,'-filter_complex_threads','2','-filter_complex',vf,'-map','[v]','-map','2:a:0','-t',f'{duration:.6f}','-r',FPS,'-c:v','libx264','-threads','3','-preset','veryfast','-crf','21','-c:a','aac','-ar','24000','-ac','1','-b:a','128k','-af','apad','-movflags','+faststart',out]);mark(out,key)
  rendered.append(out);chapter.append({'title':s['title'],'start':offset,'duration':duration,'scene':s['id']});vtts += [(offset+b,offset+e,t) for b,e,t in caps];offset+=duration
  print('Scene ready '+s['id'],flush=True)
 intro=RUN/'renders'/f'{v["id"]}-intro.mp4';source=RUN/'assets'/f'{v["id"]}-intro.mp4';key=fingerprint(['intro-v2',digest(source)])
 if not cached(intro,key):
  run(['-i',source,'-f','lavfi','-i','anullsrc=r=24000:cl=mono','-t','3','-c:v','libx264','-threads','3','-preset','veryfast','-crf','21','-pix_fmt','yuv420p','-r',FPS,'-c:a','aac','-ar','24000','-ac','1','-b:a','128k',intro]);mark(intro,key)
 listing=RUN/'assets'/f'{v["id"]}-concat.txt';listing.write_text('\n'.join("file '"+str(file).replace("'","'\\''")+"'" for file in [intro]+rendered)+'\n')
 target=RUN/'videos'/f'{v["id"]}.mp4';key=fingerprint(['concat-v2']+[digest(file) for file in [intro]+rendered])
 if not cached(target,key):run(['-f','concat','-safe','0','-i',listing,'-c','copy','-movflags','+faststart',target]);mark(target,key)
 target.with_suffix('.srt').write_text('\n\n'.join(f'{i+1}\n{stamp(b)} --> {stamp(e)}\n{t}' for i,(b,e,t) in enumerate(vtts))+'\n');target.with_suffix('.chapters.json').write_text(json.dumps(chapter,indent=2))
 streams=probe(target);assert any(x['codec_type']=='audio' for x in streams['streams']);assert any(x['codec_type']=='video' and x['width']==1920 and x['height']==1080 for x in streams['streams'])
 manifest[v['id']]={'id':v['id'],'file':str(target.relative_to(RUN)),'duration':float(streams['format']['duration']),'bytes':target.stat().st_size,'sha256':digest(target),'chapters':chapter,'captionTiming':'Phrase timing estimated within each narration scene','verifiedStreams':True};print('Assembled '+v['id'],flush=True)
 old.write_text(json.dumps(sorted(manifest.values(),key=lambda x:x['id']),indent=2)+'\n')

"""Decode final videos, validate streams/timing, and build review contact sheets."""
import argparse,json,pathlib,subprocess,math,hashlib
from PIL import Image,ImageDraw,ImageFont
p=argparse.ArgumentParser();p.add_argument('run',nargs='?',default=str(pathlib.Path(__file__).resolve().parents[2]/'output/playwright/workflow-videos-2026-09-26'));p.add_argument('--video',action='append',default=[]);a=p.parse_args();run=pathlib.Path(a.run).resolve();out=run/'qa';out.mkdir(exist_ok=True)
manifest=json.loads((run/'video-manifest.json').read_text());wanted={i for x in a.video for i in x.split(',')};report=[]
qa_path=out/'render-qa.json';prior={x['id']:x for x in json.loads(qa_path.read_text())} if qa_path.exists() else {}
def digest(path):
 h=hashlib.sha256()
 with path.open('rb') as f:
  for chunk in iter(lambda:f.read(1024*1024),b''):h.update(chunk)
 return h.hexdigest()
for v in manifest:
 if wanted and v['id'] not in wanted:continue
 source=run/v['file'];sha=digest(source);old=prior.get(v['id'],{})
 if old.get('sha256')==sha and old.get('qaVersion')==2 and old.get('fullDecodePassed') and (run/old.get('contactSheet','missing')).is_file():
  print('QA cached '+v['id'],flush=True);continue
 probe=json.loads(subprocess.check_output(['ffprobe','-v','quiet','-show_streams','-show_format','-of','json',str(source)],text=True));streams=probe['streams'];video=next(s for s in streams if s['codec_type']=='video');audio=next(s for s in streams if s['codec_type']=='audio');assert (video['width'],video['height'])==(1920,1080);assert video['r_frame_rate']=='30/1';assert audio['sample_rate']=='24000';assert audio['channels']==1
 decode=subprocess.run(['ffmpeg','-hide_banner','-v','error','-threads','2','-i',str(source),'-f','null','-'],text=True,capture_output=True);assert decode.returncode==0 and not decode.stderr,decode.stderr
 level=subprocess.run(['ffmpeg','-hide_banner','-i',str(source),'-vn','-af','volumedetect','-f','null','-'],text=True,capture_output=True);levels=[line.strip() for line in level.stderr.splitlines() if 'mean_volume:' in line or 'max_volume:' in line]
 frames=[]
 for chapter in v['chapters']:
  for label,delta in [('start',1.3),('middle',chapter['duration']/2),('end',max(.2,chapter['duration']-1.3))]:
   at=chapter['start']+delta;target=out/f'{chapter["scene"]}-{label}.jpg';subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-y','-ss',str(at),'-i',str(source),'-frames:v','1','-q:v','3',str(target)],check=True);frames.append((target,f'{chapter["scene"]} · {label} · {at:.1f}s'))
 sheet=Image.new('RGB',(1440,len(v['chapters'])*294),'#f4f0e9');d=ImageDraw.Draw(sheet)
 for i,(path,label) in enumerate(frames):
  img=Image.open(path);img.thumbnail((480,270));x=(i%3)*480;y=(i//3)*294;sheet.paste(img,(x,y));d.text((x+8,y+275),label,fill='#25221e')
 contact=out/f'{v["id"]}-contact.jpg';sheet.save(contact,quality=90)
 result={'id':v['id'],'sha256':sha,'qaVersion':2,'fullDecodePassed':True,'video':'H.264 1920x1080 30fps','audio':'AAC 24kHz mono','duration':float(probe['format']['duration']),'volume':levels,'contactSheet':str(contact.relative_to(run)),'reviewFrames':len(frames),'note':'Automated stream/decode checks; contact sheets require human/model visual inspection.'};report.append(result);print(json.dumps(result),flush=True)
 path=out/'render-qa.json';existing={x['id']:x for x in json.loads(path.read_text())} if path.exists() else {};existing.update({x['id']:x for x in report});path.write_text(json.dumps(list(existing.values()),indent=2)+'\n')

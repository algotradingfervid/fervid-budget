"""CLI-first, snapshot-driven browser recordings. No API/database fixture writes."""
import json,os,re,subprocess,time,pathlib,contextlib,hashlib
ROOT=pathlib.Path(__file__).resolve().parents[2]
RUN=pathlib.Path(os.environ.get('VIDEO_RUN',str(ROOT/'output/playwright/workflow-videos-2026-09-26')))
CLI=str(ROOT/'tools/workflow-videos/node_modules/.bin/playwright-cli')
if not pathlib.Path(CLI).exists():CLI='/Users/narendhupati/.codex/skills/playwright/scripts/playwright_cli.sh'
SESSION=os.environ.get('VIDEO_SESSION','fervid-videos')
BASE='http://127.0.0.1:8910'
for d in ['raw','screenshots','snapshots','audio','renders','assets']:(RUN/d).mkdir(parents=True,exist_ok=True)
def call(*args):
 p=subprocess.run([CLI,'-s='+SESSION,'--json',*map(str,args)],cwd=ROOT,text=True,capture_output=True,timeout=90)
 if p.returncode:raise RuntimeError('CLI '+str(args[0])+' failed: '+(p.stderr+p.stdout)[-500:])
 try:d=json.loads(p.stdout)
 except:raise RuntimeError('Invalid CLI response '+p.stdout[:150])
 def errors(x):
  if isinstance(x,dict):
   if x.get('isError'):raise RuntimeError(str(x.get('error','Browser command failed'))[:600])
   for v in x.values():errors(v)
  elif isinstance(x,list):
   for v in x:errors(v)
 errors(d);return d

def nodes(x):
 if isinstance(x,dict):
  if x.get('ref'):yield x
  for v in x.values():yield from nodes(v)
 elif isinstance(x,list):
  for v in x:yield from nodes(v)
def snapshot(name=None):
 d=call('snapshot')
 if name:(RUN/'snapshots'/f'{name}.json').write_text(json.dumps(d,indent=2))
 return list(nodes(d))
def ref(role,name,nth=0,exact=True):
 ns=snapshot();matches=[x for x in ns if x.get('role')==role and (x.get('name')==name if exact else re.search(name,x.get('name','')))]
 if len(matches)<=nth:raise RuntimeError(f'Missing {role} {name!r}; available '+str([(x.get('role'),x.get('name')) for x in ns if x.get('role') in ['button','textbox','combobox','link','radio']])[-2500:])
 return matches[nth]['ref']
def click(role,name,nth=0,exact=True):return call('click',ref(role,name,nth,exact))
def fill(name,text,nth=0):return call('fill',ref('textbox',name,nth),text)
def select(name,value,nth=0):return call('select',ref('combobox',name,nth),value)
def check(role,name):return call('check',ref(role,name))
def goto(path):return call('goto',BASE+path)
def text():return json.dumps(call('snapshot'),ensure_ascii=False)
def require(s):
 if s not in text():raise RuntimeError('Expected visible outcome not present: '+s)
def login(email='admin@fervid.local',password='admin123'):
 goto('/login');fill('Email',email);fill('Password',password);click('button','Login')
 if any(x.get('role')=='button' and x.get('name')=='Login' for x in snapshot()):raise RuntimeError('Login failed')

def plan():
 p=RUN/'storyboard.json'
 return json.loads(p.read_text()) if p.exists() else {'title':'Fervid Budget — complete workflow training','language':'English','capture':'Playwright CLI, real application with synthetic records','videos':[]}
def video(id,title,audience,features,limitations):
 d=plan();v=next((x for x in d['videos'] if x['id']==id),None)
 if v:return v
 v={'id':id,'title':title,'audience':audience,'features':features,'limitations':limitations,'scenes':[]};d['videos'].append(v);d['videos'].sort(key=lambda x:x['id']);save(d);return v
def save(d):(RUN/'storyboard.json').write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n')
@contextlib.contextmanager
def scene(video_id,key,title,narration,expected=None):
 sid=video_id+'-'+key;out=RUN/'raw'/f'{sid}.webm'
 d=plan();v=next(x for x in d['videos'] if x['id']==video_id)
 if any(x['id']==sid for x in v['scenes']):raise RuntimeError('Scene already recorded '+sid)
 call('video-start',out,'--size','1600x900','--fps','30','--cursor')
 call('video-show-actions','--duration','850','--position','top-right','--title-style','display:none','--point-style','width:14px;height:14px;border-radius:50%;background:#f1b74488;border:2px solid #a6381b','--highlight-style','outline:3px solid #f1b744;outline-offset:3px;border-radius:4px')
 good=False
 try:
  yield
  if expected:require(expected)
  time.sleep(.6)
  snap=snapshot(sid)
  call('screenshot','--filename',RUN/'screenshots'/f'{sid}.png')
  good=True
 finally:
  result=call('video-stop');call('video-hide-actions')
  (RUN/'raw'/f'{sid}.capture.json').write_text(json.dumps(result,indent=2))
 if good:
  probe=json.loads(subprocess.check_output(['ffprobe','-v','quiet','-show_format','-of','json',str(out)],text=True))
  v['scenes'].append({'id':sid,'title':title,'narration':narration,'recording':str(out.relative_to(RUN)),'screenshot':f'screenshots/{sid}.png','rawDuration':float(probe['format']['duration']),'expected':expected,'verified':True})
  save(d);print('Recorded '+sid,flush=True)

import pathlib,subprocess,json,re,time
ROOT=pathlib.Path(__file__).resolve().parents[2]
OUT=ROOT/'output/playwright/budget-workflow-2026-09-26'
CLI=ROOT/'tools/workflow-videos/node_modules/.bin/playwright-cli'
SESSION='budget-workflow';BASE='http://127.0.0.1:8930'
def call(*args):
 p=subprocess.run([str(CLI),'-s='+SESSION,'--json',*map(str,args)],cwd=ROOT,text=True,capture_output=True,timeout=90)
 if p.returncode:raise RuntimeError((p.stdout+p.stderr)[-1600:])
 d=json.loads(p.stdout)
 def err(v):
  if isinstance(v,dict):
   if v.get('isError'):raise RuntimeError(str(v))
   for a in v.values():err(a)
  elif isinstance(v,list):
   for a in v:err(a)
 err(d);return d

def nodes(x):
 if isinstance(x,dict):
  if x.get('ref'):yield x
  for v in x.values():yield from nodes(v)
 elif isinstance(x,list):
  for v in x:yield from nodes(v)
def snap(name=None):
 d=call('snapshot')
 if name:(OUT/f'{name}.json').write_text(json.dumps(d,indent=2))
 return list(nodes(d))
def ref(role,name,nth=0):
 matches=[x for x in snap() if x.get('role')==role and x.get('name')==name]
 if len(matches)<=nth:raise RuntimeError(f'Missing {role} {name}; controls '+str([(x.get('role'),x.get('name')) for x in snap() if x.get('role') in ['textbox','button','combobox','radio']]))
 return matches[nth]['ref']
def click(role,name,nth=0):return call('click',ref(role,name,nth))
def fill(name,value):return call('fill',ref('textbox',name),value)
def select(name,value):return call('select',ref('combobox',name),value)
def goto(path):return call('goto',BASE+path)
def require(text):
 for i in range(15):
  if text in json.dumps(call('snapshot'),ensure_ascii=False):return
  time.sleep(.2)
 raise AssertionError('Missing visible text: '+text)
def shot(name):snap(name);call('screenshot','--filename',OUT/f'{name}.png')
def login(email='admin@fervid.local',password='admin123'):
 goto('/login');fill('Email',email);fill('Password',password);click('button','Login')

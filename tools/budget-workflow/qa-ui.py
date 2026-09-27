import subprocess,re,json,sqlite3
from pathlib import Path
ROOT=Path.cwd(); OUT=ROOT/'output/playwright/budget-workflow-2026-09-26/independent-qa'; OUT.mkdir(parents=True,exist_ok=True)
CLI=ROOT/'tools/workflow-videos/node_modules/.bin/playwright-cli'
def cmd(*args):
 p=subprocess.run([str(CLI),'-s=budget-qa',*args],text=True,capture_output=True,timeout=90)
 if p.returncode or '### Error' in p.stdout: raise RuntimeError(p.stdout+p.stderr)
 return p.stdout
def snap(name=None):
 s=cmd('snapshot')
 if name:(OUT/(name+'.txt')).write_text(s)
 return s
def ref(role,name,n=0):
 s=snap(); r=re.findall(r'- '+re.escape(role)+r' "'+re.escape(name)+r'"[^\n]*\[ref=([^\]]+)',s)
 if len(r)<=n:raise RuntimeError('Missing '+role+' '+name+'\n'+s)
 return r[n]
def click(role,name,n=0): return cmd('click',ref(role,name,n))
def fill(name,value,n=0): return cmd('fill',ref('textbox',name,n),value)
def check(name):return cmd('check',ref('checkbox',name))
def select(name,value):
 ref('combobox',name)
 return cmd('run-code','async (page) => { await page.getByRole("combobox", {name:'+json.dumps(name)+',exact:true}).selectOption({label:'+json.dumps(value)+'}); }')
def go(path):return cmd('goto','http://127.0.0.1:8930'+path)
def shot(name):snap(name);return cmd('screenshot','--filename',str(OUT/(name+'.png')))
def db(sql):
 c=sqlite3.connect('file:'+str(ROOT/'output/playwright/budget-workflow-2026-09-26/runtime/demo.db')+'?mode=ro',uri=True);c.row_factory=sqlite3.Row
 return [dict(r) for r in c.execute(sql)]
def login(email,password='BudgetQA2026'):
 go('/login');fill('Email',email);fill('Password',password);click('button','Login')
 assert 'button "Login"' not in snap()
def evidence(name,sql):
 rows=db(sql);(OUT/(name+'.json')).write_text(json.dumps(rows,indent=2));return rows
def maintext():return snap().split('main [ref=')[-1]

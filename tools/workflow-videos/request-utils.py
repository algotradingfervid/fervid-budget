from capture import *
def controls(name=None):
 return [(n.get('role'),n.get('name'),n.get('text'),n.get('url')) for n in snapshot(name) if n.get('role') in ['textbox','combobox','button','checkbox','radio','link','heading']]
def common(title,amount='2500'):
 fill('Short title',title);select('Project','Operations');select('Head','Operations / Office Supplies');fill('Amount',amount);fill('Needed by','2026-09-30');select('Approver','Mira Shah (your default)')
def record_recovered(vid,key,title,narration,expected):
 sid=vid+'-'+key;require(expected);snapshot(sid);call('screenshot','--filename',RUN/'screenshots'/f'{sid}.png');d=plan();v=next(x for x in d['videos'] if x['id']==vid);p=json.loads(subprocess.check_output(['ffprobe','-v','quiet','-show_format','-of','json',str(RUN/'raw'/f'{sid}.webm')],text=True));v['scenes'].append({'id':sid,'title':title,'narration':narration,'recording':f'raw/{sid}.webm','screenshot':f'screenshots/{sid}.png','rawDuration':float(p['format']['duration']),'expected':expected,'verified':True});save(d)

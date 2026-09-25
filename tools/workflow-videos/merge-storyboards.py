"""Merge independently captured lessons without sharing browser or manifest writes."""
import pathlib,json,copy
ROOT=pathlib.Path(__file__).resolve().parents[2]
RUN=ROOT/'output/playwright/workflow-videos-2026-09-26'
base=RUN/'foundation-storyboard.json'
if not base.exists():base.write_text((RUN/'storyboard.json').read_text())
plan=json.loads(base.read_text());seen={v['id'] for v in plan['videos']}
for p in sorted((RUN/'agents').glob('*/storyboard.json')):
 for v in json.loads(p.read_text())['videos']:
  if not v['scenes']:continue
  if v['id'] in seen:raise RuntimeError('Duplicate video '+v['id'])
  v=copy.deepcopy(v)
  for s in v['scenes']:
   for field in ['recording','screenshot']:
    s[field]=str((p.parent/s[field]).relative_to(RUN))
   s['captureSource']=str(p.relative_to(RUN))
  plan['videos'].append(v);seen.add(v['id'])
plan['videos'].sort(key=lambda v:v['id'])
ids=[s['id'] for v in plan['videos'] for s in v['scenes']]
assert len(ids)==len(set(ids)), 'Duplicate scene IDs'
temp=RUN/'storyboard.merging.json';temp.write_text(json.dumps(plan,ensure_ascii=False,indent=2)+'\n');temp.replace(RUN/'storyboard.json')
print(f'{len(plan["videos"])} videos, {len(ids)} scenes, '+str(sum(len(s['narration'].split()) for v in plan['videos'] for s in v['scenes']))+' narration words')

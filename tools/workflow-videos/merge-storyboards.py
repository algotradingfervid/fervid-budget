"""Merge independently captured lessons without sharing browser or manifest writes."""
import pathlib, json, copy

ROOT = pathlib.Path(__file__).resolve().parents[2]
RUN = ROOT / 'output/playwright/workflow-videos-2026-09-26'

plan = {
    'title': 'Fervid Budget — complete workflow training',
    'language': 'English',
    'capture': 'Playwright CLI, real application with synthetic records',
    'videos': []
}
seen = set()

for p in sorted((RUN / 'agents').glob('*/storyboard.json')):
    d = json.loads(p.read_text())
    for v in d.get('videos', []):
        if not v.get('scenes'):
            continue
        if v['id'] in seen:
            raise RuntimeError('Duplicate video ' + v['id'])
        v = copy.deepcopy(v)
        for s in v['scenes']:
            for field in ['recording', 'screenshot']:
                if s.get(field):
                    s[field] = str((p.parent / s[field]).relative_to(RUN))
            s['captureSource'] = str(p.relative_to(RUN))
        plan['videos'].append(v)
        seen.add(v['id'])

plan['videos'].sort(key=lambda v: v['id'])
ids = [s['id'] for v in plan['videos'] for s in v['scenes']]
assert len(ids) == len(set(ids)), 'Duplicate scene IDs'
temp = RUN / 'storyboard.merging.json'
temp.write_text(json.dumps(plan, ensure_ascii=False, indent=2) + '\n')
temp.replace(RUN / 'storyboard.json')
print(f"{len(plan['videos'])} videos, {len(ids)} scenes, " + str(sum(len(s['narration'].split()) for v in plan['videos'] for s in v['scenes'])) + " narration words")

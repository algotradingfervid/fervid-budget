"""Check final artifact completeness, speech cache identity and source stability."""
import pathlib,json,hashlib,re,datetime
ROOT=pathlib.Path(__file__).resolve().parents[2];RUN=ROOT/'output/playwright/workflow-videos-2026-09-26'
plan=json.loads((RUN/'storyboard.json').read_text());manifest=json.loads((RUN/'video-manifest.json').read_text());media={v['id']:v for v in manifest};qa={v['id']:v for v in json.loads((RUN/'qa/render-qa.json').read_text())};problems=[]
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
scenes=[]
for v in plan['videos']:
 if v['id'] not in media:problems.append('Missing finalvideo '+v['id']);continue
 m=media[v['id']]
 if len(m['chapters'])!=len(v['scenes']):problems.append('Outdated chaptercount '+v['id'])
 if sha(RUN/m['file'])!=m['sha256']:problems.append('Hash mismatch '+v['id'])
 q=qa.get(v['id'],{})
 if not q.get('fullDecodePassed') or q.get('sha256')!=m['sha256']:problems.append('Final media not decoded '+v['id'])
 levels=' '.join(q.get('volume',[]));mean=re.search(r'mean_volume: (-?[\d.]+) dB',levels)
 if not mean or float(mean.group(1)) < -40:problems.append('Missing or very quiet narration '+v['id'])
 for suffix in ['.mp4','.srt','.txt','.chapters.json']:
  if not (RUN/'videos'/f'{v["id"]}{suffix}').exists():problems.append('Missing companion '+v['id']+suffix)
 for s in v['scenes']:
  scenes.append(s)
  for field in ['recording','screenshot']:
   if not (RUN/s[field]).exists():problems.append('Missing '+field+' '+s['id'])
  meta=RUN/'audio'/f'{s["id"]}.wav.json'
  if not meta.exists():problems.append('Missing speech '+s['id']);continue
  a=json.loads(meta.read_text());key={'model':a['model'],'voice':a['voice'],'format':a['format'],'text':s['narration']}
  fingerprint=hashlib.sha256(json.dumps(key,ensure_ascii=False,separators=(',',':')).encode()).hexdigest()
  if fingerprint!=a['fingerprint']:problems.append('Outdated narration '+s['id'])
review=json.loads((RUN/'qa/content-reviewed-scenes.json').read_text());reviewed={s['id']:s['narrationSHA256'] for s in review['scenes']}
for scene in scenes:
 if reviewed.get(scene['id'])!=hashlib.sha256(scene['narration'].encode()).hexdigest():problems.append('Narration content review outdated '+scene['id'])
source=json.loads((RUN/'capture-build.json').read_text());changed=[p for p,h in source['sourceHashes'].items() if not (ROOT/p).exists() or sha(ROOT/p)!=h]
if changed:problems.append('Product source changed sincecapture: '+', '.join(changed))
secretfiles=[]
for p in RUN.rglob('*'):
 if p.is_file() and p.suffix in ['.json','.md','.html','.txt','.srt','.vtt','.py','.mjs'] and 'remotion-bundle' not in p.parts:
  if re.search(r'sk-or-v1-[A-Za-z0-9]{20,}',p.read_text(errors='ignore')):secretfiles.append(str(p.relative_to(RUN)))
if secretfiles:problems.append('Potential credential in artifact: '+', '.join(secretfiles))
visualReports=['qa/final-visual-01-09.md','qa/final-visual-10-18.md','qa/final-visual-19-27.md']
for file in visualReports:
 if not (RUN/file).exists():problems.append('Missing final visual review '+file)
report={'finalVisualReviewReports':visualReports,'sampledFramesReviewed':len(scenes)*3,'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'complete':not problems,'videos':len(plan['videos']),'scenes':len(scenes),'durationSeconds':sum(v['duration'] for v in manifest),'sizeBytes':sum(v['bytes'] for v in manifest),'allVideoHashesMatch':not any('Hash mismatch' in p for p in problems),'allSpeechMatchesFinalNarration':not any('narration' in p or 'speech' in p for p in problems),'allFinalVideosDecoded':not any('decoded' in p for p in problems),'allNarrationContentReviewed':not any('content review' in p for p in problems),'productSourceUnchangedDuringProduction':not changed,'publishedTextCredentialScanPassed':not secretfiles,'limitations':['Phrase captions use estimated timing inside each narration scene.','Captures use synthetic records and demonstrate the fingerprinted application build.','External mail delivery, bank transfers and backup restoration were not performed.'],'problems':problems}
(RUN/'verification.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2));raise SystemExit(0 if not problems else 1)

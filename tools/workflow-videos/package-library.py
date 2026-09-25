import pathlib,json,zipfile,csv
ROOT=pathlib.Path(__file__).resolve().parents[2];RUN=ROOT/'output/playwright/workflow-videos-2026-09-26'
v=json.loads((RUN/'verification.json').read_text());assert v['complete'],v['problems']
plan=json.loads((RUN/'storyboard.json').read_text());media={x['id']:x for x in json.loads((RUN/'video-manifest.json').read_text())}
with (RUN/'coverage-matrix.csv').open('w',newline='') as f:
 w=csv.writer(f);w.writerow(['Lesson','Audience','Scenes','Minutes','Features','Limitations'])
 for x in plan['videos']:w.writerow([x['title'],x['audience'],len(x['scenes']),round(media[x['id']]['duration']/60,2),'; '.join(x['features']),'; '.join(x['limitations'])])
(RUN/'README.md').write_text(f'''# Fervid Budget workflow library

{v['videos']} narrated videos, {v['scenes']} recorded scenes, {v['durationSeconds']/60:.1f} minutes.

Open index.html in a browser. Search by feature or role, expand chapters, and click a chapter to play from that point. Use the player's full-screen control to read application details. MP4s, captions, transcripts and chapter files are in videos/.

This package contains the finished lessons, screenshots of their outcomes, coverage matrix, source fingerprints and verification reports. Original browser recordings, generated WAV files and reusable production scripts remain in the project workspace under output/playwright/workflow-videos-2026-09-26 and tools/workflow-videos.

All application data shown is synthetic. The narration explains current features and limitations, including manual payment evidence, recoverables not automatically posting budget expenses, offline password-help constraints, backup versus restore verification, and the misleading 0% utilisation display when budget is zero. Product source was unchanged during video production.

Tools: Playwright CLI for actual browser recording and cursor animation; Remotion for titles and screen overlays; Gemini 3.8 Flash TTS (Kore) through OpenRouter; FFmpeg for local composition; Pillow for captions. Captions use estimated phrase timing within narrated scenes. See reference/sources.md for official documentation and Remotion licensing eligibility.

video-manifest.json records final media hashes; verification.json summarizes completeness and full decoding checks. qa/content-review.md documents narration review; qa/*contact.jpg provide visual evidence sheets. This is a demonstration of the fingerprinted build, not a claim of exhaustive future regression coverage.
''')
files={RUN/'index.html',RUN/'README.md',RUN/'coverage-and-transcripts.md',RUN/'coverage-matrix.csv',RUN/'verification.json',RUN/'video-manifest.json',RUN/'capture-build.json'}
files.update((RUN/'reference').glob('*.md'))
for x in plan['videos']:
 for suffix in ['.mp4','.srt','.txt','.chapters.json']:files.add(RUN/'videos'/f'{x["id"]}{suffix}')
 for s in x['scenes']:files.add(RUN/s['screenshot'])
files.update((RUN/'qa').glob('*contact.jpg'));files.update((RUN/'qa').glob('*.md'));files.update((RUN/'qa').glob('*.json'))
archive=RUN/'fervid-workflow-library.zip'
with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=2) as z:
 for p in sorted(files):z.write(p,p.relative_to(RUN))
with zipfile.ZipFile(archive) as z:assert z.testzip() is None
print(f'Packaged {len(files)} files, {archive.stat().st_size/1024**2:.1f} MiB: {archive}')

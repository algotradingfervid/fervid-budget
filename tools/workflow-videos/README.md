# Fervid Budget workflow video production

This pipeline records the rendered application with Playwright CLI, generates Gemini narration through OpenRouter, renders Remotion titles and screen frames, and assembles local H.264/AAC MP4 videos with FFmpeg. It does not alter product source code.

The delivered library is `output/playwright/workflow-videos-2026-09-26/index.html`. Each lesson includes a video, phrase captions, a transcript, chapters, a list of demonstrated features and limits, and screenshots of the captured outcomes.

## Credentials

The root `.env` is ignored by Git and restricted to the current user. Copy `.env.example` if setting up a new checkout, then fill `OPENROUTER_API_KEY` locally. Never put credentials in a capture script, browser session, storyboard, or published archive. Narration generation reads the key only in `tts.mjs` and sends tutorial text to the OpenRouter speech endpoint. It uses the selected `google/gemini-3.8-flash-lite-tts` model and `Kore` voice. The provider accepts PCM, converted locally to 24 kHz mono WAV.

## Dependencies

Use Node.js with `process.loadEnvFile`, Python 3 with Pillow, and local `ffmpeg` / `ffprobe`. Run `npm ci --prefix tools/workflow-videos` from the repository root. Versions are pinned in this directory's package lock. Remotion downloads its renderer browser on first use.

Playwright is open source. FFmpeg and Pillow perform local assembly and caption rasterization. Remotion is source-available, with free eligibility for individuals, small teams and evaluation; it is not universally free for every commercial team. Consult [Remotion's license FAQ](https://www.remotion.dev/docs/license/faq) for your distribution context. No paid browser recording service is used. OpenRouter speech calls are billable according to the selected model; cached unchanged narration does not generate another call.

## Capturing workflows

The recordings use a dedicated seeded database served at `http://127.0.0.1:8910`, with synthetic people, vendors, requests and payments. Each parallel recorder sets its own `VIDEO_SESSION` and `VIDEO_RUN`, imports `capture.py`, and writes its own storyboard. Browser actions use freshly observed UI elements. Captures include cursor animation and a gold target highlight; automatic action titles are hidden to keep raw input values out of overlays. Scenes save the recording, a final screenshot, an accessibility snapshot and capture metadata.

Do not aim these scripts at production data. Existing scripts are records of the demonstrated workflow; fixed synthetic names and one-time actions may need a fresh isolated database when recording again. They are not idempotent migrations.

## Building the library

Run from the repository root:

```sh
python3 tools/workflow-videos/merge-storyboards.py
node tools/workflow-videos/tts.mjs
node tools/workflow-videos/render-assets.mjs
python3 tools/workflow-videos/assemble.py
python3 tools/workflow-videos/qa-render.py
python3 tools/workflow-videos/build-library.py
```

`merge-storyboards.py` preserves the foundation storyboard and combines independent agent recordings with explicit relative asset paths. Speech generation uses up to three parallel requests and caches by model, voice, format and narration text. Rendering and assembly cache by their inputs. `render-assets.mjs` and `assemble.py` accept `--video ID` (repeatable or comma-separated); `assemble.py --ready-only` processes available assets while other lessons are still being captured. QA accepts a video selection too.

The rendered UI stays at its captured timing. If narration lasts longer, the final recorded frame remains visible. Captions use estimated phrase timing within each speech scene, not forced word alignment. Each video has a three-second title and navigable chapter offsets. Captions are burned into a dedicated bottom strip outside the application viewport and also supplied as SRT.

## Verification and scope

`capture-build.json` fingerprints the application source including the already-existing uncommitted fixes. Storyboards and screenshots record UI outcomes. `video-manifest.json` records output hashes, durations and chapter locations. `qa/render-qa.json` records stream checks, full media decoding, audio levels and representative frame sheets. The final library's `verification.json` summarizes delivery completeness and source stability.

Screen recordings demonstrate the captured application build. They do not prove every role/configuration combination, real bank settlement, government tax registration validity, external email delivery, or disaster recovery. Those boundaries are explained in the relevant lessons. Examples of intended limitations include approved-request-only payment creation, manual beneficiary verification, scoped permissions, and backup creation being separate from a tested restore.

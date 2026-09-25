from capture import *
import time
call('goto','http://127.0.0.1:8920');snapshot('library-final')
counts=json.loads(call('eval','({articles:document.querySelectorAll("article").length,videos:document.querySelectorAll("video").length,pending:document.querySelectorAll(".pending").length,overflow:document.documentElement.scrollWidth>innerWidth})')['result'])
assert counts['articles']==27 and counts['videos']==27 and counts['pending']==0 and not counts['overflow'],counts
fill('Search lessons','Vendor onboarding');assert call('eval','document.querySelectorAll("article:not(.hidden)").length')['result']=='1'
snapshot();click('button','03 · Correct an invalid tax identifier');time.sleep(1)
played=json.loads(call('eval','Array.from(document.querySelectorAll("video")).filter(v=>!v.paused).map(v=>({time:v.currentTime,ready:v.readyState,error:v.error?.message||null}))')['result'])
expected=next(v for v in json.loads((RUN/'video-manifest.json').read_text()) if v['id']=='02-vendor-directory')['chapters'][2]['start'];assert len(played)==1 and played[0]['ready']>=2 and played[0]['error'] is None and abs(played[0]['time']-expected)<5,(expected,played)
call('eval','document.querySelectorAll("video").forEach(v=>v.pause())');fill('Search lessons','');call('screenshot','--filename',RUN/'qa/library-overview.png')
call('resize','390','844');snapshot('library-mobile');overflow=call('eval','document.documentElement.scrollWidth>innerWidth')['result'];assert overflow=='false',overflow
call('screenshot','--filename',RUN/'qa/library-mobile.png');call('resize','1600','900')
(RUN/'qa/library-ui.json').write_text(json.dumps({'passed':True,'counts':counts,'searchMatchedOneLesson':True,'chapterExpectedStart':expected,'chapterObservedPlayback':played,'mobileWidth':390,'mobileHorizontalOverflow':False,'testedURL':'http://127.0.0.1:8920'},indent=2)+'\n');print('Library browser QA passed')

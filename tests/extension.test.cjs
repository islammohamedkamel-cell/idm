const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
test('browser page context queues YouTube in video mode',async()=>{
 let install,click,menu,request,badge;
 const chrome={runtime:{onInstalled:{addListener(fn){install=fn}}},contextMenus:{create(m){menu=m},onClicked:{addListener(fn){click=fn}}},storage:{local:{get:async()=>({token:'paired'})}},action:{setBadgeText:async b=>{badge=b.text}}};
 vm.runInNewContext(fs.readFileSync('extension/background.js','utf8'),{chrome,console,fetch:async(url,opts)=>{request={url,...opts};return {ok:true}}});
 install();assert.ok(menu.contexts.includes('page'));
 await click({pageUrl:'https://www.youtube.com/watch?v=example'});
 assert.deepEqual(JSON.parse(request.body),{url:'https://www.youtube.com/watch?v=example',video:true});
 assert.equal(request.headers.Authorization,'Bearer paired');assert.equal(badge,'✓');
});

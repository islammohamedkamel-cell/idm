const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync('web/index.html', 'utf8');
function page(tools = {ytdlp:true, ffmpeg:true}) {
  const nodes = new Map();
  function element() { return {value:'', textContent:'', children:[], append(e){this.children.push(e)}, replaceChildren(){this.children=[]}}; }
  const calls = [];
  const document = {getElementById(id){if(!nodes.has(id))nodes.set(id,element());return nodes.get(id)},createElement:element};
  const context = {document, location:{hash:'#session',pathname:'/'},history:{replaceState(){}},URL,Date,JSON,Number,Error,setInterval(){},navigator:{clipboard:{writeText:async()=>{}}},fetch:async(path,opts)=>{
    calls.push({path,...opts});
    return {ok:true,status:200,text:async()=>JSON.stringify(path.endsWith('/tools')?tools:opts.method==='POST'?{id:'new'}:[])};
  }};
  vm.runInNewContext(html.match(/<script>([\s\S]*?)<\/script>/)[1], context);
  return {get:id=>document.getElementById(id),calls};
}
const settle=()=>new Promise(resolve=>setImmediate(resolve));
test('YouTube is the primary form and submits video jobs without a checkbox', async()=>{
  assert.ok(html.indexOf('id="youtube-add"') < html.indexOf('id="add"'));
  const p=page();await settle();
  p.get('youtube-url').value='https://youtu.be/example';
  await p.get('youtube-add').onsubmit({preventDefault(){}});
  const job=p.calls.find(c=>c.method==='POST');
  assert.deepEqual(JSON.parse(job.body),{url:'https://youtu.be/example',video:true});
  assert.equal(job.headers.Authorization,'Bearer session');
  assert.equal(p.get('youtube-url').value,'');
  assert.match(p.get('tool-status').textContent,/tools ready/);
});
test('YouTube form rejects unrelated websites',async()=>{
  const p=page();await settle();p.get('youtube-url').value='https://youtube.com.attacker.example/video';
  await p.get('youtube-add').onsubmit({preventDefault(){}});
  assert.equal(p.calls.filter(c=>c.method==='POST').length,0);
  assert.match(p.get('message').textContent,/valid YouTube/);
});
test('file workflow remains available with limits',async()=>{
  const p=page();await settle();p.get('url').value='https://example.com/file';p.get('limit').value='64';
  await p.get('add').onsubmit({preventDefault(){}});
  assert.deepEqual(JSON.parse(p.calls.find(c=>c.method==='POST').body),{url:'https://example.com/file',video:false,limit:65536});
});
test('missing tools have actionable setup guidance',async()=>{
  const p=page({ytdlp:false,ffmpeg:false});await settle();
  assert.match(p.get('tool-status').textContent,/install yt-dlp and FFmpeg on PATH/);
});

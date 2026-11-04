chrome.runtime.onInstalled.addListener(() => {chrome.contextMenus.create({id:'download',title:'Download with OpenDownload',contexts:['link','video','audio','page']});});
chrome.contextMenus.onClicked.addListener(async info => {
 const {token} = await chrome.storage.local.get('token');
 if (!token) { console.warn('Pair OpenDownload using the extension popup first'); return; }
 const url = info.linkUrl || info.srcUrl || info.pageUrl;
 try {
 const r = await fetch('http://127.0.0.1:17843/api/jobs', {method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify({url,video: /^(https?:\/\/)(www\.|m\.)?(youtube\.com|youtu\.be)\//i.test(url)})});
 if (!r.ok) throw Error(await r.text());
 await chrome.action.setBadgeText({text:'✓'});
 } catch(e) { console.error(e); await chrome.action.setBadgeText({text:'!'}); }
});

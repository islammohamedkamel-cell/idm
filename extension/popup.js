chrome.storage.local.get('token').then(v=>document.getElementById('token').value=v.token||'');
document.getElementById('save').onclick=async()=>{await chrome.storage.local.set({token:document.getElementById('token').value.trim()});document.getElementById('status').textContent='Saved. Right-click a link to send it to OpenDownload.';chrome.action.setBadgeText({text:''});};

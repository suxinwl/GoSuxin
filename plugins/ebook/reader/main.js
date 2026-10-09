import { createApp } from 'vue';
async function start() {
 const response=await fetch('/common/album/site?site='+encodeURIComponent(location.pathname),{cache:'no-store'});
 const result=await response.json();
 if(result.code!==0)throw new Error(result.message || 'Site configuration unavailable');
 window.SuxinAlbumSite=result.data;
 document.title=result.data.branding.title;
 if(result.data.branding.logo){const icon=document.createElement('link');icon.rel='icon';icon.href=result.data.branding.logo;document.head.appendChild(icon)}
 const {default:Reader}=await import('./index.js');
 createApp(Reader).mount('#app');
}
start().catch(error=>{document.getElementById('app').textContent=error.message});

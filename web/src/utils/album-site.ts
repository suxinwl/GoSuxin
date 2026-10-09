import { ref } from 'vue';
const sites=ref({zh:'/albums/',en:'/albums-en/'});
export async function loadAlbumSites(){
 const response=await fetch('/common/album/site',{cache:'no-store'});
 const result=await response.json();
 if(result.code!==0)throw new Error(result.message || '画册站点设置读取失败');
 sites.value=result.data.sites;
}
export function albumSite(language='zh') {
 const target=language==='en'?sites.value.en:sites.value.zh;
 return new URL(target,location.origin).href.replace(/\/?$/, '/')+'#/';
}

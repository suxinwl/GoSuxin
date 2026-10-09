import { computed, onBeforeUnmount, reactive } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { AlbumUploadRejected, albumGet, uploadAlbumPage } from '@/api/album';
import { ensureEditable } from './shared';

export type UploadItem = { name: string; size: number; sent: number; progress: number; progressKnown: boolean; status: 'pending'|'uploading'|'saving'|'saved'|'failed'|'unknown'; error: string };
export function useImageUpload() {
  const state = reactive({ running: false, paused: false, pauseRequested: false, albumId: 0, title: '', items: [] as UploadItem[], refreshError: '', error: '' });
  const saved = computed(() => state.items.filter(i => i.status === 'saved').length);
  let disposed = false;
  let batch: {album:any; files:File[]; can:(action:string)=>boolean; refresh:()=>Promise<void>} | undefined;
  const pending = () => state.items.some(i => i.status === 'pending');
  const leave = () => (!state.running && !pending()) || window.confirm('当前批次还有未上传文件，离开后需要重新选择这些文件。确认离开？');
  onBeforeRouteLeave(leave);
  const unload = (event: BeforeUnloadEvent) => { if (state.running || pending()) { event.preventDefault(); event.returnValue = ''; } };
  window.addEventListener('beforeunload', unload);
  onBeforeUnmount(() => { disposed = true; batch = undefined; window.removeEventListener('beforeunload', unload); });
  const reason = (e:unknown) => e instanceof Error ? e.message : '上传请求失败';
  async function refreshSaved(refresh:()=>Promise<void>) {
    // One final list refresh per run. Retry reads only; uploads with unknown outcomes are never replayed.
    for (let attempt=0; attempt<2 && !disposed; attempt++) {
      try { await refresh(); state.refreshError = ''; return; }
      catch (e) { state.refreshError = reason(e); }
      if (!attempt) await new Promise(resolve => setTimeout(resolve,1000));
    }
  }
  async function process(resuming:boolean) {
    if (state.running || !batch || !pending()) return;
    const current = batch;
    state.running = true; state.pauseRequested = false; state.error = '';
    try {
      // A read verifies the connection/login before sending any remaining files.
      if (resuming) await albumGet('permissions');
      if (!(await ensureEditable(current.album, current.can))) { state.paused = true; state.error = '未开始上传，剩余文件已保留。'; return; }
      state.paused = false;
      for (let index=0; index<current.files.length && !disposed; index++) {
        if (state.pauseRequested) break;
        const item = state.items[index];
        if (item.status !== 'pending') continue;
        item.error = ''; item.status = 'uploading';
        try {
          await uploadAlbumPage(state.albumId, {file:current.files[index]}, event => {
            if (disposed || !['uploading','saving'].includes(item.status)) return;
            if (!event.total || !Number.isFinite(event.total) || !Number.isFinite(event.loaded) || event.loaded<0) return;
            item.progressKnown = true;
            item.progress = Math.max(item.progress, Math.min(1,event.loaded/event.total));
            item.sent = Math.round(item.size*item.progress);
            if (item.progress===1) item.status='saving';
          });
          item.progressKnown=true; item.progress=1; item.sent=item.size; item.status='saved';
        } catch (e) {
          item.error=reason(e);
          if (e instanceof AlbumUploadRejected) {
            // Authentication failure rejected this file before saving; retain it for a later authenticated attempt.
            if (e.code===401 || e.code===403) {
              item.status='pending'; item.progress=0; item.sent=0; item.progressKnown=false;
              state.paused=true; state.error='登录状态已失效，请恢复登录后继续未上传文件。'; break;
            }
            item.status='failed';
          } else {
            item.status='unknown'; state.paused=pending();
            state.error='连接中断或响应异常，已停止发送后续文件。请先核对结果待确认的图片；连接恢复后可继续未上传文件。';
            break;
          }
        }
      }
      if (state.pauseRequested && pending()) { state.paused=true; state.error='已暂停，未上传文件保留在当前页面。'; }
      await refreshSaved(current.refresh);
    } catch (e) { state.paused=pending(); state.error=reason(e); }
    finally {
      state.running=false; state.pauseRequested=false;
      if (!pending()) { state.paused=false; batch=undefined; }
    }
  }
  async function run(album:any, files:File[], can:(action:string)=>boolean, refresh:()=>Promise<void>) {
    if (state.running || !files.length) return;
    if (pending() && !window.confirm('开始新批次会放弃当前未上传文件的队列，确认继续？')) return;
    state.refreshError=''; state.error=''; state.paused=false;
    state.albumId=Number(album.id); state.title=album.title || `画册 ${state.albumId}`;
    state.items=files.map(file=>({name:file.name,size:file.size,sent:0,progress:0,progressKnown:false,status:'pending',error:''}));
    batch={album,files,can,refresh};
    await process(false);
  }
  const pause = () => { if (state.running) state.pauseRequested=true; };
  const resume = () => process(true);
  return {state,saved,run,pause,resume};
}

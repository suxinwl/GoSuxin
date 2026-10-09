<template>
  <div>
    <div class="card">
      <div style="display:flex;justify-content:space-between;gap:10px;flex-wrap:wrap"><b>定时采集 / 去重设置</b><button v-if="actions['collect/auto']" class="btn sm" :disabled="startingAuto" @click="autoCollect">{{ startingAuto ? '正在启动…' : '⚡ 立即执行一次定时采集' }}</button></div>
      <div class="stat-grid" style="margin:14px 0"><div class="stat"><div class="t">定时采集状态</div><div class="n">{{ config.collect_auto_enable === '1' ? '运行中' : '已关闭' }}</div></div><div class="stat"><div class="t">上次自动执行</div><div class="n" style="font-size:15px">{{ date(config.collect_last_auto) }}</div></div><div class="stat"><div class="t">采集间隔</div><div class="n">{{ config.collect_auto_interval || 60 }} 分钟</div></div><div class="stat"><div class="t">参与接口</div><div class="n">{{ sources.filter(item => Number(item.collect_auto)).length }}</div></div></div>
      <div class="row"><div class="fi"><label>定时自动采集</label><select v-model="config.collect_auto_enable"><option value="0">关闭</option><option value="1">开启</option></select></div><div class="fi"><label>间隔（分钟，最小5）</label><input v-model="config.collect_auto_interval" type="number" min="5" /></div><div class="fi"><label>同名影片去重</label><select v-model="config.collect_dedup_title"><option value="1">合并播放地址</option><option value="0">关闭</option></select></div><div class="fi"><label>采集图片到本地</label><select v-model="config.collect_img_local"><option value="1">下载到本站</option><option value="0">用外链</option></select></div><div class="fi"><label>采集速度</label><select v-model="config.collect_speed"><option v-for="speed in speedOptions" :key="speed.value" :value="speed.value">{{ speed.label }}</option></select></div></div>
      <div class="row"><div class="fi"><label>同时采集片源数</label><select v-model="config.collect_source_concurrency"><option v-for="count in 6" :key="count" :value="String(count)">{{ count }} 个片源{{ count === 1 ? '（顺序采集）' : count === 3 ? '（推荐）' : '' }}</option></select><small class="hint">不同片源同时采集，同一片源仍按页顺序进行，并沿用上方采集速度。保存后对等待中的片源生效，已运行片源继续执行。</small></div></div>
      <button v-if="actions.saveConfig" class="btn" @click="saveConfig">保存设置</button>
    </div>
    <div v-if="jobs.length" class="card">
      <div style="display:flex;justify-content:space-between;gap:10px;flex-wrap:wrap"><b>采集进度</b><span class="hint">{{ activeJobs.length }} 个进行中任务 · {{ runningSourceCount }} 个片源正在采集</span></div>
      <p class="hint">不同片源可同时启动。刷新或重新进入页面后会恢复进度；停止任务保留已采集的数据。</p>
      <div v-for="current in visibleJobs" :key="current.id" class="collect-job" role="status" aria-live="polite">
        <div class="collect-job-heading"><b>{{ jobMode(current) }} · #{{ current.id }}</b><span class="tag" :class="isActive(current) ? 'g' : 'gr'">{{ statusText(current.status) }}</span><button v-if="isActive(current) && actions['collect/job/cancel']" class="btn plain sm" :disabled="Number(current.cancel_requested) === 1 || cancellingJobs.includes(Number(current.id))" @click="stopJob(current)">{{ Number(current.cancel_requested) === 1 || cancellingJobs.includes(Number(current.id)) ? '正在停止…' : '停止本任务' }}</button></div>
        <p class="hint">{{ date(current.started_at) }} · {{ current.source_count || sourceProgress(current).length }} 个片源 · 新增 {{ current.added || 0 }} · 更新 {{ current.updated || 0 }} · 失败 {{ current.failed || 0 }} · 屏蔽跳过 {{ current.skipped || 0 }}</p>
        <div v-for="progress in sourceProgress(current)" :key="`${current.id}-${progress.source_id}`" class="collect-source-progress">
          <div class="collect-source-heading"><b>{{ progress.source_name || '等待采集源' }}</b><span>{{ statusText(progress.status) }}</span></div>
          <p class="hint">第 {{ progress.page || 0 }}<template v-if="!hasDynamicTotal(progress)">/{{ progress.page_count || '?' }}</template> 页 · 本页 {{ progress.item || 0 }}/{{ progress.item_count ?? '?' }} 条 · 新增 {{ progress.added || 0 }} · 更新 {{ progress.updated || 0 }} · 失败 {{ progress.failed || 0 }} · 屏蔽跳过 {{ progress.skipped || 0 }}</p>
          <p v-if="hasDynamicTotal(progress)" class="hint">小柒按游标持续返回下一页，未提供确定的总页数；请查看当前页、本页条目与累计采集结果。</p>
          <div v-else-if="Number(progress.page_count) > 0 && progress.status !== 'queued'" class="collect-progress-bar"><div :style="{ width: `${sourcePercent(progress, current)}%` }" /></div>
          <p v-if="progress.error" class="hint collect-error">{{ progress.error }}</p>
        </div>
        <p v-if="current.error && !sourceProgress(current).some(progress => progress.error)" class="hint collect-error">{{ current.error }}</p>
      </div>
      <button v-if="jobs.length > activeJobs.length + 3" class="btn plain sm" @click="showRecentJobs = !showRecentJobs">{{ showRecentJobs ? '收起历史任务' : '查看近期任务' }}</button>
    </div>
    <div class="card"><div style="display:flex;justify-content:space-between;gap:10px;flex-wrap:wrap"><b>采集接口</b><div v-if="permissions.save"><button class="btn plain sm" @click="editErciyuan">二次元</button> <button class="btn plain sm" @click="editYqk">小柒 APP</button> <button class="btn sm" @click="edit()">+ 添加接口</button></div></div>
      <p class="hint">小柒 APP 通过一个聚合接口采集，各播放线路会合并到影片中。动态入口 JSON 地址可在“系统设置 → 站点信息”中配置。</p>
      <div class="tb-wrap"><table class="tb"><thead><tr><th>ID</th><th>名称</th><th>接口地址</th><th>备注</th><th>状态</th><th>操作</th></tr></thead><tbody><tr v-for="item in sources" :key="item.id"><td>{{ item.id }}</td><td>{{ item.name }}</td><td>{{ item.api_url }}</td><td>{{ item.remark }}</td><td><span class="tag" :class="Number(item.status) ? 'g' : 'gr'">{{ Number(item.status) ? '启用' : '停用' }}</span></td><td><button v-if="actions['collect/preview'] || actions['collect/classes']" class="btn sm" :disabled="!Number(item.status)" :title="!Number(item.status) ? '请先启用采集源' : ''" @click="selectSource(item)">进入采集</button> <button v-if="actions['collect/job/start']" class="btn plain sm" :disabled="sourceBusy(item.id) || !Number(item.status)" @click="run(item.id)">{{ sourceBusy(item.id) ? '采集中…' : '快速采集' }}</button> <button v-if="permissions.save" class="btn plain sm" @click="edit(item)">编辑</button> <button v-if="permissions.delete" class="btn plain sm" @click="remove(item.id)">删除</button></td></tr><tr v-if="!sources.length"><td colspan="6" class="hint">暂无采集接口</td></tr></tbody></table></div>
    </div>
    <div v-if="editing" class="card"><h3>{{ editor.id ? '编辑接口' : '添加接口' }}</h3><form class="form" @submit.prevent="saveSource"><div class="fi"><label>名称 *</label><input v-model="editor.name" type="text" required /></div><div class="fi"><label>接口地址（JSON 或内置片源）*</label><input v-model="editor.api_url" type="text" :readonly="['hongguo://app','4kvm://site','yqk://app','erciyuan://app'].includes(editor.api_url)" required /><small v-if="editor.api_url === 'yqk://app'" class="hint">小柒只需保留这一个接口。服务地址从动态入口 JSON 自动读取，各线路及实际清晰度随影片更新。</small><small v-if="editor.api_url === 'erciyuan://app'" class="hint">二次元自动读取动漫分类与影片，定时采集每类近期一页，各播放线路与已有影片安全合并。</small></div><div class="fi"><label>备注</label><input v-model="editor.remark" type="text" /></div><div class="row"><div class="fi"><label>状态</label><select v-model="editor.status"><option value="1">启用</option><option value="0">停用</option></select></div><div class="fi"><label>自动采集</label><select v-model="editor.collect_auto"><option value="1">开启</option><option value="0">关闭</option></select></div><div class="fi"><label>增量小时</label><input v-model="editor.collect_hours" type="number" min="1" /></div></div><button class="btn">保存</button> <button class="btn plain" type="button" @click="editing = false">取消</button></form></div>
    <div v-if="activeSource" class="card">
      <b>接口采集 - {{ activeSource.name }}</b>
      <div class="searchbar" style="margin-top:14px">
        <button v-if="actions['collect/classes']" class="btn plain sm" :disabled="loadingClasses" @click="loadClasses">{{ loadingClasses ? '正在拉取分类…' : '拉取资源站分类' }}</button>
        <select v-model="typeId" @change="changeClass(typeId)"><option value="0">全部资源</option><option v-for="item in classes" :key="item.type_id" :value="String(item.type_id)">{{ item.type_name }}</option></select>
        <button v-if="actions['collect/preview']" class="btn plain sm" :disabled="loadingPreview" @click="preview">{{ loadingPreview ? '预览中…' : '预览' }}</button>
        <input v-model.number="page" type="number" min="1" style="width:90px" />
        <select v-model="hours"><option value="0">全量</option><option value="6">近6小时</option><option value="12">近12小时</option><option value="24">近24小时</option></select>
        <button v-if="actions['collect/job/start']" class="btn sm" :disabled="sourceBusy(activeSource.id)" @click="run(activeSource.id)">开始连续采集</button>
        <button v-if="actions['collect/job/start']" class="btn plain sm" :disabled="sourceBusy(activeSource.id)" @click="run(activeSource.id,true)">只采本页</button>
      </div>
      <p v-if="classStatus" class="hint" role="status">{{ classStatus }}</p>
      <div v-if="classes.length" class="searchbar" aria-label="资源站分类">
        <button class="btn sm" :class="typeId === '0' ? '' : 'plain'" @click="changeClass('0')">全部资源</button>
        <button v-for="item in classes" :key="item.type_id" class="btn sm" :class="typeId === String(item.type_id) ? '' : 'plain'" @click="changeClass(String(item.type_id))">{{ item.type_name }}</button>
      </div>
      <p v-if="resultText" class="hint" role="status">{{ resultText }}</p>
      <p v-if="previewHasDynamicTotal" class="hint">第 {{ previewData.page || page }} 页 · 本页 {{ (previewData.list || []).length }} 条 · {{ Number(previewData.pagecount || 1) > Number(previewData.page || page) ? '还有下一页' : '暂无下一页' }}。小柒未提供确定的资源总数和总页数。</p>
      <p v-else class="hint">资源总数 {{ previewData.total || 0 }} · 第 {{ previewData.page || page }} / {{ previewData.pagecount || 1 }} 页</p>
      <div class="tb-wrap"><table class="tb"><thead><tr><th>片名</th><th>分类</th><th>备注</th></tr></thead><tbody><tr v-for="item in previewData.list || []" :key="item.vod_id"><td>{{ item.vod_name }}</td><td>{{ item.type_name }}</td><td>{{ item.vod_remarks }}</td></tr><tr v-if="!loadingPreview && !(previewData.list || []).length"><td colspan="3" class="hint">暂无预览资源</td></tr></tbody></table></div>
      <div class="searchbar" style="margin-top:12px"><button class="btn plain sm" :disabled="loadingPreview || page <= 1" @click="changePage(-1)">上一页</button><button class="btn plain sm" :disabled="loadingPreview || page >= Number(previewData.pagecount || 1)" @click="changePage(1)">下一页</button></div>
    </div>
    <div v-if="actions['collect/duplicates']" class="card"><b>重复影片合并</b><p class="hint">扫描归一化片名与年份相近的影片，合并播放地址、评论、收藏与播放记录。</p><button class="btn sm" @click="scanDuplicates">🔍 扫描疑似重复影片</button><div v-for="group in duplicates" :key="group.key" style="margin-top:10px;padding:10px;border:1px solid var(--line);border-radius:8px">{{ group.name }}：{{ group.ids.join('、') }} <button v-if="actions['collect/merge']" class="btn plain sm" @click="mergeDuplicate(group)">合并</button></div></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
import { defHttp } from '@/utils/http';
import { Message, Modal } from '@arco-design/web-vue';
const props=defineProps<{permissions:Record<string,boolean>;actions:Record<string,boolean>}>();
const config = reactive<Record<string, string>>({}); const sources = ref<Record<string, any>[]>([]); const editor = reactive<Record<string, any>>({});
const editing = ref(false); const activeSource = ref<Record<string, any> | null>(null); const classes = ref<Record<string, any>[]>([]);
const loadingClasses = ref(false); const loadingPreview = ref(false); const classStatus = ref('');
let classesRequest = 0; let previewRequest = 0;
const typeId = ref('0'); const page = ref(1); const hours = ref('24'); const previewData = reactive<Record<string, any>>({}); const resultText = ref(''); const duplicates = ref<Record<string, any>[]>([]);
const speedOptions = [{value:'gentle',label:'温和（间隔 2.5 秒）'},{value:'slow',label:'慢速（间隔 1.5 秒）'},{value:'normal',label:'标准（间隔 0.8 秒）'},{value:'fast',label:'快速（间隔 0.2 秒）'}];
type CollectJob = Record<string, any>;
const jobs = ref<CollectJob[]>([]);
const startingAuto = ref(false);
const startingSources = ref<number[]>([]);
const cancellingJobs = ref<number[]>([]);
const showRecentJobs = ref(false);
const isYqkSource=(source:Record<string,any>|null|undefined)=>String(source?.api_url||'').trim().toLowerCase()==='yqk://app';
const previewHasDynamicTotal=computed(()=>isYqkSource(activeSource.value));
const isActive = (value: CollectJob) => ['queued', 'running'].includes(String(value.status));
const activeJobs = computed(() => jobs.value.filter(isActive));
const visibleJobs = computed(() => {
  const recent = jobs.value.filter(current => !isActive(current));
  return [...activeJobs.value, ...(showRecentJobs.value ? recent : recent.slice(0, 3))];
});
const sourceProgress = (current: CollectJob): CollectJob[] => Array.isArray(current.source_progress) && current.source_progress.length
  ? current.source_progress
  : Number(current.source_id) > 0 ? [current] : [];
const activeSourceProgress = computed(() => activeJobs.value.flatMap(sourceProgress).filter(isActive));
const runningSourceCount = computed(() => new Set(activeSourceProgress.value.filter(current => current.status === 'running').map(current => Number(current.source_id))).size);
const sourceBusy = (id: number) => {
  const sourceId=Number(id);
  const sourceURL=String(sources.value.find(source=>Number(source.id)===sourceId)?.api_url || '').trim();
  const sameSource=(otherId:number)=>otherId===sourceId || Boolean(sourceURL && sourceURL===String(sources.value.find(source=>Number(source.id)===otherId)?.api_url || '').trim());
  return startingSources.value.some(sameSource) || activeSourceProgress.value.some(progress=>sameSource(Number(progress.source_id)));
};
const statusLabels: Record<string, string> = { queued: '等待采集', running: '正在采集', complete: '采集完成', partial: '部分完成', failed: '采集失败', cancelled: '已停止', interrupted: '采集已中断' };
const statusText = (status: any) => statusLabels[String(status)] || '等待开始';
const jobMode = (current: CollectJob) => current.mode === 'manual' ? '手动采集' : '定时批量采集';
const hasDynamicTotal = (progress: CollectJob) => progress.dynamic_total === true || Number(progress.dynamic_total) === 1 || isYqkSource(sources.value.find(source => Number(source.id) === Number(progress.source_id)));
function sourcePercent(progress: CollectJob, current: CollectJob) {
  if (progress.status === 'complete') return 100;
  const itemCount = Number(progress.item_count) || 0;
  const itemFraction = itemCount > 0 ? Math.min(1, Number(progress.item) / itemCount) : 0;
  if (Number(progress.one_page ?? current.one_page) === 1) return Math.round(itemFraction * 100);
  const start = Math.max(1, Number(progress.start_page) || Number(current.start_page) || 1);
  const pageCount = Math.max(1, Number(progress.page_count) - start + 1);
  return Math.min(100, Math.max(0, Math.round((Math.max(0, Number(progress.page) - start) + itemFraction) / pageCount * 100)));
}
function mergeJobs(data: CollectJob, replace = false) {
  const incoming: CollectJob[] = [...(data.running_jobs || []), ...(data.jobs || [])];
  if (Number(data.id) > 0 && !incoming.some(current => Number(current.id) === Number(data.id))) incoming.push(data);
  const unique = new Map<number, CollectJob>();
  for (const current of replace ? incoming : [...incoming, ...jobs.value]) {
    const id = Number(current.id);
    if (id > 0 && !unique.has(id)) unique.set(id, current);
  }
  jobs.value = [...unique.values()].sort((left, right) => Number(right.id) - Number(left.id));
}
let pollTimer:ReturnType<typeof setInterval>|undefined;
let polling = false;
let disposed = false;
const date = (v: any) => Number(v) ? new Date(Number(v)*1000).toLocaleString('zh-CN') : '从未执行';
async function load(refreshConfig = true) { const values = await defHttp.get({url:'/suxinvideo/config'}); if(refreshConfig){Object.assign(config,values); if(!speedOptions.some(item=>item.value===config.collect_speed))config.collect_speed='gentle'; const concurrency = Number(config.collect_source_concurrency); config.collect_source_concurrency = String(Number.isInteger(concurrency) && concurrency >= 1 && concurrency <= 6 ? concurrency : 3);}else{config.collect_last_auto=values.collect_last_auto;config.collect_last_result=values.collect_last_result;} const data=await defHttp.get({url:'/suxinvideo/list',params:{table:'collect_api',page:1}});sources.value=data.list||[]; }
function edit(item: Record<string, any> = {}) { Object.keys(editor).forEach(key=>delete editor[key]);Object.assign(editor,{id:item.id||0,name:item.name||'',api_url:item.api_url||'',remark:item.remark||'',status:String(item.status??1),collect_auto:String(item.collect_auto??0),collect_hours:String(item.collect_hours??12)});editing.value=true; }
function editYqk(){const existing=sources.value.find(item=>String(item.api_url).trim().toLowerCase()==='yqk://app');edit(existing||{name:'小柒 APP',api_url:'yqk://app',remark:'聚合片源，自动读取动态入口并合并影片播放线路'});}
function editErciyuan(){const existing=sources.value.find(item=>String(item.api_url).trim()==='erciyuan://app');edit(existing||{name:'二次元',api_url:'erciyuan://app',remark:'星次元动漫片源；分类及影片动态读取，分集播放时解析',collect_auto:'1',collect_hours:'12'});}
async function saveSource(){const data={name:editor.name,api_url:editor.api_url,remark:editor.remark,status:editor.status,collect_auto:editor.collect_auto,collect_hours:editor.collect_hours};await defHttp.post({url:'/suxinvideo/save',params:{table:'collect_api',id:editor.id,data}});editing.value=false;Message.success('保存成功');await load();}
function remove(id:number){Modal.confirm({title:'确认删除采集源？',content:'将删除此采集接口配置。',onOk:async()=>{await defHttp.post({url:'/suxinvideo/delete',params:{table:'collect_api',id}});await load();}});}
async function saveConfig(){const values={collect_auto_enable:config.collect_auto_enable,collect_auto_interval:config.collect_auto_interval,collect_dedup_title:config.collect_dedup_title,collect_img_local:config.collect_img_local,collect_speed:config.collect_speed,collect_source_concurrency:config.collect_source_concurrency};await defHttp.post({url:'/suxinvideo/saveConfig',params:{values}});Message.success('已保存');}
async function autoCollect(){if(startingAuto.value)return;startingAuto.value=true;try{const current=await defHttp.post({url:'/suxinvideo/collect/auto',params:{}});if(current)mergeJobs(current);Message.success('批量采集任务已启动，正在采集的片源会自动跳过');await pollJob();}catch(error){Message.error(error instanceof Error?error.message:'启动采集失败');}finally{startingAuto.value=false;}}
async function pollJob(){
  if(!props.actions['collect/job/status'] || polling || disposed)return;
  polling=true;
  try{
    const current=await defHttp.get({url:'/suxinvideo/collect/job/status',params:{}});
    if(current && !disposed){
      const previouslyActive=new Set(activeJobs.value.map(item=>Number(item.id)));
      mergeJobs(current,true);
      const finished=jobs.value.filter(item=>previouslyActive.has(Number(item.id)) && !isActive(item));
      if(finished.length){Message.info(finished.map(item=>`任务 #${item.id}：${statusText(item.status)}`).join('；'));await load(false);}
    }
  }catch(error){resultText.value=error instanceof Error?error.message:'读取采集进度失败';}
  finally{polling=false;}
}
async function stopJob(current:CollectJob){
  const id=Number(current.id);
  if(!id || Number(current.cancel_requested) === 1 || cancellingJobs.value.includes(id))return;
  cancellingJobs.value=[...cancellingJobs.value,id];
  current.cancel_requested=1;
  try{await defHttp.post({url:'/suxinvideo/collect/job/cancel',params:{id:current.id}});Message.info(`正在停止任务 #${current.id}，已采集数据将保留`);await pollJob();}
  catch(error){current.cancel_requested=0;Message.error(error instanceof Error?error.message:'停止采集失败');}
  finally{cancellingJobs.value=cancellingJobs.value.filter(jobId=>jobId!==id);}
}
function selectSource(item:Record<string,any>){activeSource.value=item;classes.value=[];typeId.value='0';page.value=1;classStatus.value='';Object.keys(previewData).forEach(key=>delete previewData[key]);if(props.actions['collect/classes'])void loadClasses();else if(props.actions['collect/preview'])void preview();}
async function loadClasses(){const sourceId=activeSource.value?.id;if(!sourceId)return;const request=++classesRequest;loadingClasses.value=true;classStatus.value='正在连接资源站并读取分类…';try{const result=await defHttp.get({url:'/suxinvideo/collect/classes',params:{api_id:sourceId}});if(activeSource.value?.id!==sourceId||request!==classesRequest)return;classes.value=result.list||[];classStatus.value=classes.value.length?isYqkSource(activeSource.value)?`已拉取 ${classes.value.length} 个分类；小柒未提供确定的资源总数`:`已拉取 ${classes.value.length} 个分类，资源站资源总数 ${result.total||0}`:'资源站未返回分类，请检查接口是否支持 ac=list';if(props.actions['collect/preview'])void preview();}catch(error){if(activeSource.value?.id!==sourceId||request!==classesRequest)return;classStatus.value=error instanceof Error?error.message:'拉取资源站分类失败';Message.error(classStatus.value);}finally{if(request===classesRequest)loadingClasses.value=false;}}
function changeClass(id:string){typeId.value=id;page.value=1;void preview();}
function changePage(delta:number){page.value=Math.max(1,page.value+delta);void preview();}
async function preview(){const sourceId=activeSource.value?.id;if(!sourceId)return;const request=++previewRequest;loadingPreview.value=true;const requestedPage=Math.max(1,Number(page.value)||1);const requestedType=typeId.value;try{const result=await defHttp.get({url:'/suxinvideo/collect/preview',params:{api_id:sourceId,page:requestedPage,type_id:requestedType}});if(activeSource.value?.id!==sourceId||request!==previewRequest)return;Object.assign(previewData,result);page.value=Number(result.page)||requestedPage;resultText.value='';}catch(error){if(activeSource.value?.id!==sourceId||request!==previewRequest)return;const message=error instanceof Error?error.message:'预览资源失败';resultText.value=message;Message.error(message);}finally{if(request===previewRequest)loadingPreview.value=false;}}
async function run(id:number, once=false){
  const sourceId=Number(id);
  if(sourceBusy(sourceId))return;
  startingSources.value=[...startingSources.value,sourceId];
  resultText.value='正在启动采集任务…';
  const selected=Number(activeSource.value?.id)===sourceId;
  try{
    const current=await defHttp.post({url:'/suxinvideo/collect/job/start',params:{api_id:sourceId,page:selected?Math.max(1,Number(page.value)||1):1,type_id:selected?Number(typeId.value):0,hours:Number(hours.value),one_page:once}});
    if(current)mergeJobs(current);
    resultText.value=`任务 #${current?.id || ''} 已启动，可继续采集其它片源或稍后回来查看进度`;
    await pollJob();
  }catch(error){resultText.value=error instanceof Error?error.message:'启动采集失败';Message.error(resultText.value);}
  finally{startingSources.value=startingSources.value.filter(source=>source!==sourceId);}
}
async function scanDuplicates(){const result=await defHttp.get({url:'/suxinvideo/collect/duplicates'});duplicates.value=result.list||[];}
function mergeDuplicate(group:Record<string,any>){Modal.confirm({title:`合并“${group.name}”？`,content:'将合并这些影片及关联播放来源。',onOk:async()=>{await defHttp.post({url:'/suxinvideo/collect/merge',params:{ids:group.ids}});await scanDuplicates();}});}
onMounted(async()=>{await load();await pollJob();if(!disposed)pollTimer=setInterval(pollJob,1200);});
onUnmounted(()=>{disposed=true;if(pollTimer)clearInterval(pollTimer);});
</script>
<style scoped>
.collect-job{margin-top:14px;padding:14px;border:1px solid var(--line);border-radius:8px;}
.collect-job-heading,.collect-source-heading{display:flex;align-items:center;gap:10px;flex-wrap:wrap;}
.collect-job-heading button{margin-left:auto;}
.collect-source-progress{margin-top:12px;padding:12px;background:#f8f9fc;border-radius:6px;}
.collect-source-heading{justify-content:space-between;}
.collect-progress-bar{height:8px;background:#eef0f5;border-radius:6px;overflow:hidden;}
.collect-progress-bar div{height:100%;background:#e5322d;transition:width .3s;}
.collect-error{color:#c52525;white-space:pre-wrap;overflow-wrap:anywhere;}
@media(max-width:600px){.collect-job{padding:10px;}.collect-source-progress{padding:10px;}.collect-job-heading button{margin-left:0;}}
</style>

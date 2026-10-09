<template>
  <div>
    <div v-if="editing" :class="pageEditor ? 'card' : 'modal open'" @click.self="closeEdit">
      <div :class="pageEditor ? '' : 'mbox'">
      <h3>{{ form.id ? '编辑' : '添加' }}{{ config.title }}</h3>
      <form class="form" :class="{'form-vod':kind==='vod','form-article':kind==='article'}" @submit.prevent="save">
        <div v-for="field in config.fields" v-show="kind !== 'user' || ((field.key !== 'vip_days' || !form.id) && (field.key !== 'vip_expire' || !!form.id) && (field.key !== 'email' || !form.id) && (field.key !== 'status' || !!form.id))" :key="field.key" class="fi" :class="{'wide':field.long||field.key==='pic'||field.key==='play_from'}">
          <label>{{ field.label }}</label>
          <textarea v-if="field.long" v-model="form[field.key]" :style="field.key === 'play_url' ? 'min-height:120px;font-family:monospace' : kind==='article' ? 'min-height:300px' : ''" />
          <select v-else-if="field.options" v-model="form[field.key]"><option v-for="option in field.options" :key="option.value" :value="option.value">{{ option.label }}</option></select>
          <input v-else :type="field.type || 'text'" v-model="form[field.key]" :required="field.required" :min="field.min" :max="field.max" />
        </div>
        <div class="form-actions"><button class="btn" type="submit">保存</button> <button class="btn plain" type="button" @click="closeEdit">{{ pageEditor ? '返回列表' : '取消' }}</button></div>
      </form>
      </div>
    </div>
    <div v-show="!editing || !pageEditor" class="card">
      <div class="searchbar">
        <input v-model="keyword" type="text" :placeholder="config.search || '搜索'" @keyup.enter="search" />
        <select v-if="kind === 'vod'" v-model="typeId"><option value="0">全部分类</option><option v-for="item in types" :key="item.id" :value="String(item.id)">{{ item.name }}</option></select>
        <select v-if="kind === 'order' || kind === 'comment'" v-model="statusFilter"><option value="-1">全部状态</option><option value="0">{{ kind === 'order' ? '待支付' : '待审核' }}</option><option value="1">{{ kind === 'order' ? '已支付' : '已显示' }}</option><option v-if="kind === 'order'" value="2">已取消</option></select>
        <button class="btn sm" @click="search">搜索</button>
        <button v-if="config.create && permissions.save" class="btn sm" @click="edit()">+ 添加{{ config.title }}</button>
        <button v-if="config.bulk && permissions.deleteBatch" class="btn sm" @click="deleteSelected">🗑 删除选中</button>
        <button v-if="kind === 'user' && actions['user/unlock']" class="btn plain sm" @click="unlock">解除登录锁定</button>
        <template v-if="kind === 'order' && permissions.cleanup"><button v-for="option in orderCleanup" :key="option.label" class="btn plain sm" @click="cleanup(option)">{{ option.label }}</button></template>
        <template v-if="kind === 'comment' && permissions.cleanup"><button class="btn plain sm" @click="cleanup({label:'待审核评论',mode:0})">清理待审核</button><button class="btn plain sm" @click="cleanup({label:'全部评论',mode:1})">清空全部</button></template>
      </div>
      <div class="tb-wrap"><table class="tb"><thead><tr><th v-if="config.bulk"><input type="checkbox" :checked="!!rows.length && selected.length === rows.length" @change="selectAll" /></th><th>ID</th><th v-for="column in config.columns" :key="column.key">{{ column.label }}</th><th>操作</th></tr></thead><tbody>
        <tr v-for="item in rows" :key="item.id">
          <td v-if="config.bulk"><input v-model="selected" type="checkbox" :value="Number(item.id)" /></td><td>{{ item.id }}</td>
          <td v-for="column in config.columns" :key="column.key">
            <a v-if="kind === 'vod' && column.key === 'name'" :href="`/suxinvideo/detail?id=${item.id}`" target="_blank" style="color:var(--blue)">{{ item.name }}</a>
            <span v-else-if="kind === 'vod' && column.key === 'tname'">{{ item.tname || '-' }}</span>
            <span v-else-if="column.key === 'show_home'" class="tag" :class="Number(item.show_home) ? 'g' : 'gr'">{{ Number(item.show_home) ? '是' : '否' }}</span>
            <span v-else-if="column.key === 'status' && kind === 'order'" class="tag" :class="Number(item.status) === 1 ? 'g' : 'gr'">{{ ['待支付','已支付','已取消'][Number(item.status)] || '-' }}</span>
            <span v-else-if="column.key === 'status' && kind === 'comment'" class="tag" :class="Number(item.status) ? 'g' : 'gr'">{{ Number(item.status) ? '已显示' : '待审核' }}</span>
            <span v-else-if="column.key === 'status'" class="tag" :class="Number(item.status) ? 'g' : 'gr'">{{ Number(item.status) ? (kind==='vod' ? '上架' : kind==='type' ? '显示' : '启用') : (kind==='vod' ? '下架' : kind==='type' ? '隐藏' : '停用') }}</span>
            <span v-else-if="column.key === 'vip'" class="tag" :class="Number(item.vip) ? 'r' : 'gr'">{{ Number(item.vip) ? 'VIP' : '免费' }}</span>
            <span v-else-if="column.key === 'amount'">¥{{ Number(item.amount || 0).toFixed(2) }}</span>
            <span v-else-if="column.key === 'created' || column.key === 'updatetime' || column.key === 'reg_time'">{{ date(item[column.key]) }}</span>
            <span v-else>{{ item[column.key] ?? '-' }}</span>
          </td>
          <td><button v-if="config.fields.length && kind !== 'order' && permissions.save" class="btn plain sm" @click="edit(item)">编辑</button> <button v-if="config.remove && permissions.delete" class="btn plain sm" @click="remove(item.id)">删除</button> <button v-if="kind === 'comment' && permissions.save" class="btn plain sm" @click="setCommentStatus(item)">{{ Number(item.status) ? '隐藏' : '通过' }}</button> <button v-if="kind === 'film_request' && Number(item.status) !== 1 && permissions.save" class="btn plain sm" @click="markDone(item.id)">标记已处理</button></td>
        </tr>
        <tr v-if="!rows.length"><td :colspan="config.columns.length + 2 + (config.bulk ? 1 : 0)" class="hint">暂无数据</td></tr>
      </tbody></table></div>
      <div v-if="paginated" class="sx-page"><button :disabled="page <= 1" @click="page--;load()">上一页</button><button class="cur">第 {{ page }} 页</button><button :disabled="page * pageSize >= total" @click="page++;load()">下一页</button><span class="hint">共 {{ total }} 条</span></div>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import { defHttp } from '@/utils/http';
import { useRoute, useRouter } from 'vue-router';

type Field = { key: string; label: string; long?: boolean; type?: string; required?: boolean; min?: number; max?: number; options?: { value: string; label: string }[] };
type Config = { title: string; search?: string; columns: Field[]; fields: Field[]; create?: boolean; remove?: boolean; bulk?: boolean };
const f = (key: string, label: string, more: Partial<Field> = {}): Field => ({ key, label, ...more });
const state = [ { value: '1', label: '启用' }, { value: '0', label: '停用' } ];
const configs: Record<string, Config> = {
  vod: { title: '影片', search: '按片名搜索', create: true, remove: true, bulk: true, columns: [f('name','片名'),f('tname','分类'),f('remarks','备注'),f('vip','付费'),f('total_hits','热度'),f('status','状态'),f('updatetime','更新时间')], fields: [f('name','片名 *',{required:true}),f('sub','副名'),f('type_id','分类 ID',{type:'number'}),f('remarks','备注'),f('class','类型'),f('year','年份'),f('area','地区'),f('lang','语言'),f('score','评分'),f('director','导演'),f('actor','演员'),f('pic','封面图地址'),f('content','简介',{long:true}),f('play_from','播放来源标识'),f('play_url','播放地址',{long:true}),f('vip','观看权限',{options:[{value:'0',label:'免费观看'},{value:'1',label:'VIP 专享'}]}),f('points','积分购买',{type:'number',min:0}),f('status','状态',{options:state})] },
  type: { title:'分类',create:true,remove:true,columns:[f('name','名称'),f('c','影片数'),f('sort','排序'),f('status','状态'),f('show_home','首页显示')],fields:[f('name','名称 *',{required:true}),f('pid','上级分类 ID',{type:'number'}),f('sort','排序',{type:'number'}),f('show_home','首页展示',{options:state}),f('icon','图标'),f('image','图片'),f('status','状态',{options:state})] },
  slide: { title:'幻灯',create:true,remove:true,columns:[f('name','标题'),f('pic','图片'),f('url','链接'),f('pos','位置'),f('sort','排序'),f('status','状态')],fields:[f('name','标题 *',{required:true}),f('pic','图片地址'),f('url','链接'),f('pos','位置'),f('sort','排序',{type:'number'}),f('status','状态',{options:state})] },
  article: { title:'文章公告',search:'按标题搜索',create:true,remove:true,columns:[f('title','标题'),f('status','状态'),f('addtime','发布时间')],fields:[f('title','标题 *',{required:true}),f('content','内容',{long:true}),f('status','状态',{options:state})] },
  link: { title:'友情链接',create:true,remove:true,columns:[f('name','名称'),f('url','网址'),f('sort','排序'),f('status','状态')],fields:[f('name','名称 *',{required:true}),f('url','网址 *',{required:true}),f('sort','排序',{type:'number'}),f('status','状态',{options:state})] },
  film_request: { title:'求片',columns:[f('title','片名'),f('user_id','会员 ID'),f('note','备注'),f('status','状态'),f('created','提交时间')],fields:[f('note','备注',{long:true}),f('status','处理状态',{options:state})],remove:true },
  player: { title:'播放器',create:true,remove:true,columns:[f('code','代码'),f('name','名称'),f('parse','解析地址'),f('status','状态')],fields:[f('code','代码 *',{required:true}),f('name','名称 *',{required:true}),f('parse','解析地址'),f('status','状态',{options:state})] },
  user: { title:'会员',search:'邮箱/昵称',create:true,remove:true,bulk:true,columns:[f('email','邮箱'),f('name','昵称'),f('points','积分'),f('vip_expire','VIP到期'),f('status','状态'),f('reg_time','注册时间')],fields:[f('email','邮箱',{type:'email'}),f('name','昵称'),f('password','密码',{type:'password'}),f('points','积分',{type:'number',min:0}),f('vip_expire','VIP到期日',{type:'date'}),f('vip_days','开通VIP天数',{type:'number',min:0}),f('status','状态',{options:state})] },
  goods: { title:'充值套餐',create:true,remove:true,columns:[f('name','名称'),f('price','价格'),f('points','积分'),f('days','会员天数'),f('status','状态')],fields:[f('name','名称 *',{required:true}),f('price','价格',{type:'number',min:0}),f('points','积分',{type:'number',min:0}),f('days','会员天数',{type:'number',min:0}),f('sort','排序',{type:'number'}),f('status','状态',{options:state})] },
  order: { title:'订单',remove:true,columns:[f('order_no','订单号'),f('uname','会员'),f('title','套餐'),f('amount','金额'),f('pay_type','方式'),f('status','状态'),f('trade_no','第三方单号'),f('created','时间')],fields:[] },
  comment: { title:'评论',remove:true,columns:[f('uname','用户'),f('vname','影片'),f('content','内容'),f('status','状态'),f('created','时间')],fields:[f('content','内容',{long:true}),f('status','审核状态',{options:state})] },
};
const props = defineProps<{ kind: string; permissions: Record<string, boolean>; actions: Record<string, boolean> }>();
const route=useRoute();const router=useRouter();
const pageEditor=computed(()=>route.query.view==='content/vod'||route.query.view==='content/article');
const config = computed(() => configs[props.kind] || configs.vod);
const keyword = ref(''); const page = ref(1); const total = ref(0); const pageSize = ref(20); const paginated = ref(false); const rows = ref<Record<string, any>[]>([]);
const typeId = ref('0'); const types = ref<Record<string, any>[]>([]); const selected = ref<number[]>([]);
const statusFilter = ref('-1');
const orderCleanup = [{label:'清理全部待支付',status:0,days:0},{label:'清理7天前待支付',status:0,days:7},{label:'清理30天前待支付',status:0,days:30},{label:'清理全部已取消',status:2,days:0}];
const editing = ref(false); const form = reactive<Record<string, any>>({});
const date = (v: any) => v ? new Date(Number(v)*1000).toLocaleString('zh-CN') : '-';
function search() { page.value = 1; load(); }
async function load() {
  const result = await defHttp.get({ url:'/suxinvideo/list', params:{ table:props.kind, page:page.value, keyword:keyword.value, type_id:typeId.value, status:statusFilter.value } });
  rows.value = result.list || []; total.value = result.total || 0; pageSize.value = result.pageSize || 20; paginated.value = !!result.paginated; selected.value = [];
  if(props.kind==='vod'){types.value=result.types||[];const field=config.value.fields.find(item=>item.key==='type_id');if(field)field.options=types.value.map(item=>({value:String(item.id),label:item.name}));}
}
function edit(item: Record<string, any> = {}, fromRoute=false) {
  if(pageEditor.value&&!fromRoute){router.push({path:'/suxinvideo/admin',query:{...route.query,edit:item.id||'new'}});return;}
  Object.keys(form).forEach(key => delete form[key]); form.id = item.id || 0;
  config.value.fields.forEach(field => {
    const defaults: Record<string,string> = {status:'1',show_home:'0',vip:'0',points:'0',days:'0',sort:'0',pid:'0',vip_days:'0'};
    form[field.key] = item[field.key] == null ? (defaults[field.key] ?? '') : String(item[field.key]);
  });
  if (props.kind === 'user' && Number(item.vip_expire) > 0) { const d=new Date(Number(item.vip_expire)*1000); form.vip_expire = `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`; }
  editing.value = true;
}
function closeEdit(){if(pageEditor.value){const query={...route.query};delete query.edit;router.push({path:'/suxinvideo/admin',query});}else editing.value=false;}
watch(()=>route.query.edit,async value=>{if(!pageEditor.value)return;if(!value){editing.value=false;return;}if(value==='new'){edit({},true);return;}const id=Number(value);if(!Number.isInteger(id)||id<1){Message.error('无效的编辑地址');closeEdit();return;}const result=await defHttp.get({url:'/suxinvideo/list',params:{table:props.kind,id,page:1}});if(!result.list?.length){Message.error('内容不存在');closeEdit();return;}edit(result.list[0],true);},{immediate:true});
async function save() {
  const data: Record<string, any> = {};
  config.value.fields.forEach(field => { if (form[field.key] !== '' && !(field.key === 'password' && form.id)) data[field.key] = form[field.key]; });
  if (props.kind === 'user' && form.id) { delete data.vip_days; data.vip_expire = form.vip_expire ? Math.floor(new Date(`${form.vip_expire}T00:00:00`).getTime()/1000) : 0; }
  if (props.kind === 'user') await defHttp.post({ url:'/suxinvideo/user/save', params:{ id:form.id, data, password:form.password } });
  else await defHttp.post({ url:'/suxinvideo/save', params:{ table:props.kind, id:form.id, data } });
  Message.success('保存成功'); closeEdit(); await load();
}
async function remove(id: number) {
  Modal.confirm({ title:'确认删除？', content:'删除后不可恢复', onOk: async () => { if (props.kind === 'user') await defHttp.post({url:'/suxinvideo/user/delete',params:{id}}); else await defHttp.post({url:'/suxinvideo/delete',params:{table:props.kind,id}}); Message.success('删除成功'); await load(); } });
}
function selectAll(event: Event) { selected.value = (event.target as HTMLInputElement).checked ? rows.value.map(item => Number(item.id)) : []; }
function deleteSelected() {
  if (!selected.value.length) { Message.warning('请先勾选'); return; }
  Modal.confirm({title:`确认删除选中的 ${selected.value.length} 条记录？`,content:'删除后不可恢复。',onOk:async()=>{await defHttp.post({url:'/suxinvideo/deleteBatch',params:{table:props.kind,ids:selected.value}});Message.success('删除成功');await load();}});
}
async function markDone(id: number) { await defHttp.post({url:'/suxinvideo/save',params:{table:'film_request',id,data:{status:1}}}); await load(); }
async function setCommentStatus(item: Record<string,any>) { await defHttp.post({url:'/suxinvideo/save',params:{table:'comment',id:item.id,data:{status:Number(item.status)?0:1}}}); await load(); }
function cleanup(option: Record<string,any>) { Modal.confirm({title:`确认清理${option.label}？`,content:'此操作不可恢复',onOk:async()=>{const result=await defHttp.post({url:'/suxinvideo/cleanup',params:{table:props.kind,...option}});Message.success(`已清理 ${result.deleted} 条`);await load();}}); }
async function unlock() { await defHttp.post({url:'/suxinvideo/user/unlock',params:{}}); Message.success('登录锁定已解除'); }
onMounted(load);
</script>

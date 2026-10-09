<template>
  <div class="container">
    <page-card breadcrumb scrollPage>
      <div class="repo-header">
        <div><h2>私有插件仓</h2><p>集中保存团队源码，按版本发布与下载。</p></div>
        <a-space wrap><a-button @click="openCategories">管理分类</a-button><a-button type="primary" @click="edit()">新增代码</a-button></a-space>
      </div>
      <a-space wrap class="filters">
        <a-input-search v-model="keyword" placeholder="搜索标题或标识" style="width:260px" @search="search" @press-enter="search" />
        <a-select v-model="categoryId" placeholder="全部分类" allow-clear style="width:180px" @change="search">
          <a-option v-for="cat in categories" :key="cat.id" :value="Number(cat.id)">{{ cat.name }}</a-option>
        </a-select>
        <a-select v-model="status" placeholder="全部状态" allow-clear style="width:130px" @change="search"><a-option value="0">草稿</a-option><a-option value="1">已发布</a-option></a-select>
        <a-button :loading="loading" @click="load">刷新</a-button>
      </a-space>
      <a-alert v-if="loadError" type="error" style="margin-bottom:16px">列表加载失败，请刷新重试。</a-alert>
      <a-table :data="items" row-key="id" :loading="loading" :pagination="{current:page,pageSize:20,total,showTotal:true}" :scroll="{x:1000}" @page-change="changePage">
        <template #columns>
          <a-table-column title="代码" :width="240"><template #cell="{record}"><strong>{{record.title}}</strong><div class="muted">{{record.name}}</div></template></a-table-column>
          <a-table-column title="分类" :width="130"><template #cell="{record}">{{categoryName(record.cid)}}</template></a-table-column>
          <a-table-column title="说明" data-index="des" />
          <a-table-column title="当前版本" :width="110"><template #cell="{record}">{{record.version || '尚未上传'}}</template></a-table-column>
          <a-table-column title="状态" :width="100"><template #cell="{record}"><a-tag :color="Number(record.status)===1?'green':'gray'">{{Number(record.status)===1?'已发布':'草稿'}}</a-tag></template></a-table-column>
          <a-table-column title="下载" data-index="download" :width="75" />
          <a-table-column title="操作" :width="255"><template #cell="{record}"><a-space size="mini">
            <a-button type="text" @click="details(record)">版本</a-button>
            <a-button v-if="record.can_edit" type="text" @click="edit(record)">编辑</a-button>
            <a-button v-if="record.can_edit" type="text" @click="upload(record)">上传</a-button>
            <a-button v-if="record.can_edit" type="text" status="danger" @click="remove(record)">删除</a-button>
          </a-space></template></a-table-column>
        </template>
      </a-table>
      <a-alert class="repo-notice">新增资料后上传 ZIP 源码包。发布后的代码可供有下载权限的成员使用；每个版本独立保留。</a-alert>
    </page-card>

    <a-modal v-model:visible="editVisible" :title="form.id?'编辑代码资料':'新增代码'" :on-before-ok="save" :mask-closable="false" :width="650">
      <a-form :model="form" layout="vertical">
        <a-form-item label="标题" required><a-input v-model="form.title" :max-length="120" /></a-form-item>
        <a-form-item label="代码标识" required><a-input v-model="form.name" placeholder="如 my_plugin，使用字母、数字、下划线或短横线" :max-length="64" /></a-form-item>
        <a-form-item label="分类" required><a-select v-model="form.cid"><a-option v-for="cat in categories" :key="cat.id" :value="Number(cat.id)">{{cat.name}}</a-option></a-select></a-form-item>
        <a-form-item label="说明"><a-textarea v-model="form.des" :max-length="1000" /></a-form-item>
        <a-form-item label="使用说明"><a-textarea v-model="form.content" :auto-size="{minRows:3,maxRows:8}" :max-length="20000" /></a-form-item>
        <a-form-item label="作者"><a-input v-model="form.author" :max-length="80" /></a-form-item>
        <a-form-item v-if="form.id" label="发布状态"><a-switch v-model="published"><template #checked>发布</template><template #unchecked>草稿</template></a-switch></a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:visible="uploadVisible" :title="'上传版本 · '+selected?.title" :on-before-ok="saveUpload" :mask-closable="false">
      <a-form :model="releaseForm" layout="vertical">
        <a-form-item label="版本号" required><a-input v-model="releaseForm.version" placeholder="如 1.0.0，每个版本号只能上传一次" :max-length="32" /></a-form-item>
        <a-form-item label="版本说明"><a-textarea v-model="releaseForm.note" :max-length="1000" /></a-form-item>
        <a-form-item label="源码包（ZIP，不超过 128 MB）" required><input type="file" accept=".zip" @change="selectFile" /></a-form-item>
        <a-form-item label="上传后发布"><a-switch v-model="releaseForm.publish" /></a-form-item>
      </a-form>
      <a-alert v-if="uploading">正在上传并校验源码包，请等待完成。</a-alert>
    </a-modal>

    <a-drawer v-model:visible="detailVisible" :width="850" :footer="false" :title="detail?.title || '版本管理'">
      <a-spin :loading="detailLoading" style="width:100%">
        <template v-if="detail">
          <a-descriptions :column="2" bordered><a-descriptions-item label="标识">{{detail.name}}</a-descriptions-item><a-descriptions-item label="当前版本">{{detail.version || '尚未上传'}}</a-descriptions-item><a-descriptions-item label="作者">{{detail.author || '—'}}</a-descriptions-item><a-descriptions-item label="分类">{{categoryName(detail.cid)}}</a-descriptions-item></a-descriptions>
          <p>{{detail.des}}</p><p class="instructions">{{detail.content}}</p>
          <a-table :data="releases" :pagination="false" row-key="id">
            <template #columns>
              <a-table-column title="版本"><template #cell="{record}"><strong>{{record.version}}</strong><div class="muted">{{record.note}}</div></template></a-table-column>
              <a-table-column title="大小" :width="100"><template #cell="{record}">{{fileSize(record.size)}}</template></a-table-column>
              <a-table-column title="上传时间" data-index="createtime" :width="180" />
              <a-table-column title="下载" data-index="download" :width="65" />
              <a-table-column title="操作" :width="85"><template #cell="{record}"><a-button type="text" :loading="downloading===record.id" @click="download(record)">下载</a-button></template></a-table-column>
            </template>
          </a-table>
        </template>
      </a-spin>
    </a-drawer>

    <a-modal v-model:visible="cateVisible" title="分类管理" :footer="false" :width="700">
      <a-table :data="categories" :pagination="false" row-key="id" size="small">
        <template #columns><a-table-column title="名称" data-index="name" /><a-table-column title="说明" data-index="remark" /><a-table-column title="排序" data-index="weigh" :width="70" /><a-table-column title="操作" :width="130"><template #cell="{record}"><a-button type="text" @click="Object.assign(cateForm,record)">编辑</a-button><a-button type="text" status="danger" @click="removeCategory(record)">删除</a-button></template></a-table-column></template>
      </a-table>
      <a-divider>{{cateForm.id?'编辑分类':'新增分类'}}</a-divider>
      <a-form :model="cateForm" layout="vertical"><a-form-item label="名称" required><a-input v-model="cateForm.name" :max-length="80" /></a-form-item><a-form-item label="说明"><a-input v-model="cateForm.remark" :max-length="500" /></a-form-item><a-form-item label="排序"><a-input-number v-model="cateForm.weigh" /></a-form-item></a-form>
      <a-space><a-button type="primary" :loading="cateSaving" @click="saveCategory">保存分类</a-button><a-button @click="resetCategory">清空</a-button></a-space>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import { repoGet, repoPost, uploadRelease, downloadRelease, type Category, type PackageRecord, type Release } from './api';

const items=ref<PackageRecord[]>([]),categories=ref<Category[]>([]),total=ref(0),page=ref(1),keyword=ref(''),categoryId=ref<number>(),status=ref<string>();
const loading=ref(false),loadError=ref(false),editVisible=ref(false),published=ref(false);
const emptyForm=()=>({id:0,cid:0,title:'',name:'',des:'',content:'',author:'',status:0});
const form=reactive(emptyForm());
const categoryName=(id:number)=>categories.value.find(c=>Number(c.id)===Number(id))?.name || '未分类';
const loadCategories=async()=>{categories.value=await repoGet<Category[]>('cate/list');};
async function load(){loading.value=true;loadError.value=false;try{const result=await repoGet('content/list',{page:page.value,pageSize:20,keyword:keyword.value,cid:categoryId.value||0,status:status.value});items.value=result.items;total.value=Number(result.total);}catch{loadError.value=true;}finally{loading.value=false;}}
function search(){page.value=1;load();}function changePage(value:number){page.value=value;load();}
async function edit(record?:PackageRecord){try{const item=record?(await repoGet('content/detail',{id:record.id})).item:emptyForm();Object.assign(form,item,{cid:Number(item.cid),id:Number(item.id),status:Number(item.status)});published.value=Number(item.status)===1;editVisible.value=true;}catch{}}
async function save(){if(!form.title.trim()||!form.cid||!/^\w[\w-]{0,63}$/.test(form.name)){Message.warning('请填写标题、有效分类和代码标识');return false;}try{await repoPost('content/save',{...form,status:form.id&&published.value?1:0});Message.success('资料已保存');await load();return true;}catch{return false;}}
function remove(record:PackageRecord){Modal.confirm({title:'删除代码资料',content:`将删除“${record.title}”及其全部源码版本，确认继续？`,onOk:async()=>{await repoPost('content/delete',{id:record.id});await load();}});}
const uploadVisible=ref(false),uploading=ref(false),selected=ref<PackageRecord>(),uploadFile=ref<File>();
const releaseForm=reactive({version:'',note:'',publish:true});
function upload(record:PackageRecord){selected.value=record;releaseForm.version='';releaseForm.note='';releaseForm.publish=true;uploadFile.value=undefined;uploadVisible.value=true;}
function selectFile(event:Event){uploadFile.value=(event.target as HTMLInputElement).files?.[0];}
async function saveUpload(){if(uploading.value)return false;if(!selected.value||!uploadFile.value||!/^[A-Za-z0-9][A-Za-z0-9._-]{0,31}$/.test(releaseForm.version)){Message.warning('请选择源码包并填写有效版本号');return false;}if(uploadFile.value.size>128*1024*1024){Message.warning('源码包不能超过 128 MB');return false;}uploading.value=true;try{await uploadRelease(selected.value.id,releaseForm.version,releaseForm.note,releaseForm.publish,uploadFile.value);Message.success('版本已保存');await load();return true;}catch(error){Message.error((error as Error).message);return false;}finally{uploading.value=false;}}
const detailVisible=ref(false),detailLoading=ref(false),detail=ref<PackageRecord>(),releases=ref<Release[]>([]),downloading=ref<number>();
async function details(record:PackageRecord){detailVisible.value=true;detailLoading.value=true;detail.value=undefined;releases.value=[];try{const result=await repoGet('content/detail',{id:record.id});detail.value=result.item;releases.value=result.releases;}catch{}finally{detailLoading.value=false;}}
const fileSize=(size:number)=>Number(size)<1024*1024?(Number(size)/1024).toFixed(1)+' KB':(Number(size)/1024/1024).toFixed(1)+' MB';
async function download(release:Release){downloading.value=release.id;try{await downloadRelease(release);if(detail.value)await details(detail.value);await load();}catch(error){Message.error((error as Error).message);}finally{downloading.value=undefined;}}
const cateVisible=ref(false),cateSaving=ref(false),cateForm=reactive({id:0,name:'',remark:'',weigh:0});
function resetCategory(){Object.assign(cateForm,{id:0,name:'',remark:'',weigh:0});}
async function openCategories(){resetCategory();await loadCategories();cateVisible.value=true;}
async function saveCategory(){if(!cateForm.name.trim()){Message.warning('请输入分类名称');return;}cateSaving.value=true;try{await repoPost('cate/save',{...cateForm});resetCategory();await loadCategories();Message.success('分类已保存');}catch{}finally{cateSaving.value=false;}}
function removeCategory(record:Category){Modal.confirm({title:'删除分类',content:`确认删除“${record.name}”？分类下存在代码时无法删除。`,onOk:async()=>{await repoPost('cate/delete',{id:record.id});await loadCategories();await load();}});}
onMounted(async()=>{try{await loadCategories();}catch{}await load();});
</script>

<style scoped>
.repo-header{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:16px;margin-bottom:24px}.repo-header h2{margin:0 0 8px;color:var(--color-text-1)}.repo-header p,.muted{color:var(--color-text-3)}.filters{margin-bottom:20px}.muted{margin-top:5px;font-size:12px}.repo-notice{margin-top:20px}.instructions{white-space:pre-wrap;line-height:1.8;color:var(--color-text-2)}
</style>

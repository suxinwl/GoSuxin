<template>
  <div class="site-settings">
    <a-card title="画册站点设置" :loading="loading">
      <a-alert>设置仅用于电子画册。Logo、品牌文案和官网链接可按语言分别配置；官网留空时不显示官网入口。保存后刷新画册页面即可生效。</a-alert>
      <a-form :model="form" layout="vertical" style="max-width:900px; margin-top:24px" @submit-success="save">
        <a-row :gutter="20">
          <a-col :span="12"><a-form-item label="中文画册网站地址"><a-input v-model="form.chineseURL" placeholder="/albums/ 或 https://你的域名/catalogue/" /></a-form-item></a-col>
          <a-col :span="12"><a-form-item label="英文画册网站地址"><a-input v-model="form.englishURL" placeholder="/albums-en/ 或 https://你的英文域名/" /></a-form-item></a-col>
        </a-row>
        <p class="help">地址不包含 #。支持同一网站下的不同路径，或使用不同域名；自定义域名须先完成 DNS、HTTPS 和反向代理配置，并指向当前服务。不能占用后台及 API 路径。</p>
        <a-tabs>
          <a-tab-pane v-for="lang in languages" :key="lang.key" :title="lang.label">
            <a-form-item label="品牌 Logo">
              <a-space wrap>
                <img v-if="form[lang.key].logo" :src="form[lang.key].logo" class="logo-preview" alt="Logo 预览" />
                <a-upload v-if="can('site/logo')" accept="image/png,image/jpeg,image/webp" :show-file-list="false" :custom-request="uploadLogoFor(lang.key)"><template #upload-button><a-button :loading="uploading">上传 Logo</a-button></template></a-upload>
                <a-button v-if="form[lang.key].logo" @click="form[lang.key].logo = ''">移除 Logo</a-button>
              </a-space>
            </a-form-item>
            <a-form-item label="Logo 地址"><a-input v-model="form[lang.key].logo" allow-clear placeholder="上传图片，或填写 HTTPS 图片地址" /></a-form-item>
            <p class="help">支持 PNG、JPG、WEBP，最大 5 MB。Logo 保持原始比例，同时用作画册浏览器图标；不设置时显示品牌名称。</p>
            <a-row :gutter="20"><a-col :span="12"><a-form-item label="品牌名称"><a-input v-model="form[lang.key].name" :max-length="80" /></a-form-item></a-col><a-col :span="12"><a-form-item label="浏览器标题"><a-input v-model="form[lang.key].title" :max-length="120" /></a-form-item></a-col></a-row>
            <a-form-item label="首页标题"><a-input v-model="form[lang.key].headline" :max-length="200" /></a-form-item>
            <a-form-item label="品牌介绍"><a-textarea v-model="form[lang.key].description" :max-length="1000" /></a-form-item>
            <a-form-item label="官网链接"><a-input v-model="form[lang.key].website" allow-clear placeholder="https://你的官网域名；留空隐藏" /></a-form-item>
            <a-form-item label="页脚版权信息"><a-input v-model="form[lang.key].copyright" allow-clear :max-length="300" /></a-form-item>
          </a-tab-pane>
        </a-tabs>
        <a-space><a-button v-if="can('site/save')" type="primary" html-type="submit" :loading="saving">保存设置</a-button><a-button @click="load">重新加载</a-button><a-link :href="albumSite('zh')" target="_blank">查看中文站</a-link><a-link :href="albumSite('en')" target="_blank">查看英文站</a-link></a-space>
      </a-form>
    </a-card>
  </div>
</template>
<script setup lang="ts">
import { reactive, ref, onMounted } from 'vue';
import type { RequestOption } from '@arco-design/web-vue/es/upload/interfaces';
import { Message } from '@arco-design/web-vue';
import { albumGet, albumPost, uploadSiteLogo } from '@/api/album';
import { albumSite, loadAlbumSites } from '@/utils/album-site';
import { useAlbumAccess } from '../shared';
type Brand = {name:string;title:string;headline:string;description:string;website:string;copyright:string;logo:string};
const emptyBrand = ():Brand => ({name:'',title:'',headline:'',description:'',website:'',copyright:'',logo:''});
const form=reactive({chineseURL:'/albums/',englishURL:'/albums-en/',chinese:emptyBrand(),english:emptyBrand()});
const languages: {key:'chinese'|'english';label:string}[]=[{key:'chinese',label:'中文品牌'},{key:'english',label:'English branding'}];
const loading=ref(false), saving=ref(false), uploading=ref(false);
const {can}=useAlbumAccess();
async function load(){loading.value=true;try{Object.assign(form,await albumGet('site/get'));await loadAlbumSites()}finally{loading.value=false}}
async function save(){saving.value=true;try{await albumPost('site/save',form);await loadAlbumSites();Message.success('站点设置已保存，刷新画册页面后生效')}finally{saving.value=false}}
function uploadLogoFor(key:'chinese'|'english'){return (options:RequestOption)=>uploadLogo(options,key)}
function uploadLogo(options:RequestOption,key:'chinese'|'english'){
 let cancelled=false;
 (async()=>{uploading.value=true;try{const result=await uploadSiteLogo({file:options.fileItem.file as Blob});if(result.code!==0)throw new Error(result.message);if(!cancelled){form[key].logo=result.data.url;options.onSuccess(result);Message.success('Logo 已上传，请保存设置')}}catch(error){options.onError(error);Message.error(error instanceof Error?error.message:'Logo 上传失败')}finally{uploading.value=false}})();
 return {abort(){cancelled=true}};
}
onMounted(load);
</script>
<style scoped>.site-settings{padding:20px}.help{color:var(--color-text-3);line-height:1.8;margin:0 0 20px}.logo-preview{max-width:220px;height:64px;object-fit:contain;background:var(--color-fill-2);padding:8px}</style>

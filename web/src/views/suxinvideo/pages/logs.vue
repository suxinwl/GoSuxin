<template><div class="card"><div class="searchbar" style="justify-content:space-between"><span class="hint">记录后台敏感操作，供安全审计使用。</span><button v-if="actions['logs/clear']" class="btn plain sm" @click="clear">清空日志</button></div><div class="tb-wrap"><table class="tb"><thead><tr><th>时间</th><th>管理员</th><th>操作</th><th>IP</th></tr></thead><tbody><tr v-for="item in rows" :key="item.id"><td>{{ new Date(Number(item.created)*1000).toLocaleString('zh-CN') }}</td><td>{{ item.username || item.admin_id }}</td><td>{{ item.action }}</td><td>{{ item.ip }}</td></tr><tr v-if="!rows.length"><td colspan="4" class="hint">暂无日志</td></tr></tbody></table></div><div class="sx-page"><button :disabled="page<=1" @click="page--;load()">上一页</button><button class="cur">第 {{ page }} 页</button><button :disabled="page*30>=total" @click="page++;load()">下一页</button></div></div></template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';import { Modal } from '@arco-design/web-vue';import { defHttp } from '@/utils/http';
defineProps<{actions:Record<string,boolean>}>();
const rows=ref<Record<string,any>[]>([]);const total=ref(0);const page=ref(1);
async function load(){const result=await defHttp.get({url:'/suxinvideo/logs',params:{page:page.value}});rows.value=result.list||[];total.value=result.total||0;}
function clear(){Modal.confirm({title:'确定清空全部 CMS 安全日志？',content:'将永久删除影视插件的安全日志。',onOk:async()=>{await defHttp.post({url:'/suxinvideo/logs/clear',params:{}});page.value=1;await load();}});}
onMounted(load);
</script>

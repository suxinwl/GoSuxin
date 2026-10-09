<template>
  <div class="card">
    <div class="card-head"><h2>客户端发布</h2><a class="btn plain sm" href="/suxinvideo/app-download" target="_blank">查看 APP 下载页</a></div>
    <p class="hint">发布手机／平板版与 TV 版签名 APK。服务器校验包名、签名、版本及 SHA256；同一类型保持原签名，确保用户可以覆盖升级。</p>
    <form v-if="actions['clients/publish']" class="release-form" @submit.prevent="publish">
      <label>客户端类型<select v-model="form.platform"><option value="mobile">手机／平板</option><option value="tv">Android TV</option></select></label>
      <label>版本名称<input v-model="form.version_name" maxlength="40" placeholder="1.0.0" required /></label>
      <label>版本号<input v-model.number="form.version_code" type="number" min="1" required /></label>
      <label>最低系统 API<input v-model.number="form.min_sdk" type="number" min="23" max="100" required /></label>
      <label class="wide">更新说明<textarea v-model="form.changelog" rows="3" maxlength="16000" placeholder="填写本次更新内容" /></label>
      <label class="wide">签名 APK<input ref="fileInput" type="file" accept=".apk" @change="selectFile" required /></label>
      <div class="wide"><button class="btn" :disabled="busy || !file">{{ busy ? '正在上传并校验…' : '校验并发布' }}</button></div>
    </form>
    <p v-if="error" class="release-error" role="alert">{{ error }}</p>
    <div class="table-wrap"><table class="table"><thead><tr><th>类型</th><th>版本</th><th>系统</th><th>大小</th><th>发布状态</th><th>更新说明</th><th>操作</th></tr></thead><tbody>
      <tr v-for="item in releases" :key="item.id"><td>{{ item.platform === 'tv' ? 'TV' : '手机／平板' }}</td><td><b>{{ item.version_name }}</b><br><small>{{ item.version_code }}</small></td><td>API {{ item.min_sdk }}+</td><td>{{ (item.size / 1048576).toFixed(1) }} MB</td><td><span class="tag" :class="item.status ? 'g' : 'r'">{{ item.status ? '已发布' : '已下架' }}</span></td><td style="max-width:280px;white-space:pre-line">{{ item.changelog || '—' }}</td><td><a v-if="item.status" class="btn plain sm" :href="item.url">下载</a> <button v-if="actions['clients/status']" class="btn plain sm" @click="changeStatus(item)">{{ item.status ? '下架' : '重新发布' }}</button><details><summary>文件校验</summary><code class="release-hash">{{ item.sha256 }}</code></details></td></tr>
      <tr v-if="!releases.length"><td colspan="7" class="hint">尚无已发布客户端</td></tr>
    </tbody></table></div>
    <section class="release-guide"><h3>网易爆米花接入</h3><p class="hint">局域网内选择 Jellyfin，服务器地址 192.168.10.10，HTTP 端口 8601。使用网站会员邮箱和密码；影片会员／积分权限沿用 CMS。接入教程显示在 APP 下载页中。</p></section>
  </div>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import { defHttp } from '@/utils/http';
defineProps<{ actions: Record<string, boolean> }>();
interface Release { id: number; platform: string; version_name: string; version_code: number; min_sdk: number; size: number; status: number; url: string; sha256: string; changelog: string }
const releases = ref<Release[]>([]);
const form = reactive({ platform: 'mobile', version_name: '1.0.0', version_code: 1, min_sdk: 23, changelog: '' });
const file = ref<File>(); const fileInput = ref<HTMLInputElement>(); const busy = ref(false); const error = ref('');
async function load() { const data = await defHttp.get({ url: '/suxinvideo/clients/releases' }); releases.value = data.list || []; }
function selectFile(event: Event) { file.value = (event.target as HTMLInputElement).files?.[0]; error.value = ''; }
async function publish() { if (!file.value || busy.value) return; busy.value = true; error.value = ''; try {
  if (file.value.size > 512 * 1048576) throw Error('APK 超过 512MB');
  const result = await defHttp.uploadFile({ url: '/admin/suxinvideo/clients/publish' }, { name: 'file', file: file.value, filename: file.value.name, data: { ...form } });
  if (result.code !== 0) throw Error(result.message || '发布失败'); Message.success('客户端已发布'); await load(); file.value = undefined; if (fileInput.value) fileInput.value.value = '';
} catch (e: any) { error.value = e.message || '上传或校验失败'; } finally { busy.value = false; } }
function changeStatus(item: Release) { Modal.confirm({ title: item.status ? '确认下架此版本？' : '确认重新发布此版本？', content: '下载页和 APP 更新提示将按发布状态显示。', onOk: async () => { await defHttp.post({ url: '/suxinvideo/clients/status', params: { id: item.id, status: item.status ? 0 : 1 } }); await load(); } }); }
onMounted(load);
</script>
<style scoped>
.release-form{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px;margin:24px 0;padding:20px;background:#f8f9fb;border-radius:10px}.release-form label{display:flex;flex-direction:column;gap:8px;font-size:13px}.release-form input,.release-form select,.release-form textarea{padding:9px;border:1px solid #d9dce3;border-radius:6px;min-width:0}.wide{grid-column:1/-1}.release-error{color:#b42318;padding:12px;background:#fff1ef;border-radius:8px}.release-hash{display:block;max-width:240px;overflow-wrap:anywhere;font-size:11px;margin-top:8px}.release-guide{margin-top:24px;padding-top:16px;border-top:1px solid var(--line)}@media(max-width:700px){.release-form{grid-template-columns:1fr 1fr}}@media(max-width:420px){.release-form{grid-template-columns:1fr}}
</style>

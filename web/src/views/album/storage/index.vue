<template>
  <div class="storage-settings">
    <a-card title="画册存储设置" :loading="loading">
      <a-alert>默认使用本地存储。123 云盘连接仅供画册使用，保存后需验证连接，再启用；已有文件保留原存储位置。</a-alert>
      <a-space class="actions"><a-tag color="green">当前：{{ activeName }}</a-tag><a-button v-if="can('storage/activate')" :disabled="active === 'local'" @click="activate('local')">使用本地存储</a-button><a-button @click="load">刷新</a-button></a-space>
      <a-table :data="profiles" :pagination="false" row-key="id">
        <template #columns>
          <a-table-column title="连接名称" data-index="name" />
          <a-table-column title="中文目录 ID" data-index="parentId" />
          <a-table-column title="英文目录 ID" data-index="englishParentId" />
          <a-table-column title="验证状态"><template #cell="{ record }">{{ record.verifiedAt ? '已验证' : '待验证' }}</template></a-table-column>
          <a-table-column title="操作"><template #cell="{ record }"><a-space><a-button v-if="can('storage/save')" size="small" @click="edit(record)">编辑</a-button><a-button v-if="can('storage/test')" size="small" :loading="busy === record.id" @click="test(record.id)">验证</a-button><a-button v-if="can('storage/activate')" size="small" :disabled="!record.verifiedAt || active === record.id" @click="activate(record.id)">启用</a-button></a-space></template></a-table-column>
        </template>
      </a-table>
      <a-form v-if="can('storage/save')" :model="form" layout="vertical" class="connection" @submit-success="save">
        <h3>{{ form.id ? '编辑连接' : '新增 123 云盘连接' }}</h3>
        <a-form-item label="连接名称" field="name" :rules="[{ required: true, message: '请输入连接名称' }]"><a-input v-model="form.name" /></a-form-item>
        <a-form-item label="Client ID" field="clientID" :rules="[{ required: true, message: '请输入 Client ID' }]"><a-input v-model="form.clientID" /></a-form-item>
        <a-form-item label="Client Secret"><a-input-password v-model="form.clientSecret" :placeholder="form.id ? '留空保留已保存密钥' : '请输入密钥'" autocomplete="new-password" /></a-form-item>
        <a-row :gutter="20"><a-col :span="12"><a-form-item label="中文目录 ID"><a-input-number v-model="form.parentId" :min="0" /></a-form-item></a-col><a-col :span="12"><a-form-item label="英文目录 ID（可选）"><a-input-number v-model="form.englishParentId" :min="0" allow-clear /></a-form-item></a-col></a-row>
        <a-form-item label="CDN 鉴权密钥"><a-input-password v-model="form.cdnKey" :placeholder="form.id ? '留空保留已保存密钥' : '请输入密钥'" autocomplete="new-password" /></a-form-item>
        <a-form-item label="URL 鉴权"><a-switch v-model="form.urlAuth" /></a-form-item>
        <a-space><a-button type="primary" html-type="submit" :loading="saving">保存连接</a-button><a-button @click="reset">新增连接</a-button></a-space>
      </a-form>
    </a-card>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { Message } from '@arco-design/web-vue';
import { albumGet, albumPost } from '@/api/album';
import { useAlbumAccess } from '../shared';
type Profile = { id: string; name: string; clientID: string; parentId: number; englishParentId?: number | null; verifiedAt: number; urlAuth: boolean };
const { can } = useAlbumAccess();
const active = ref('local'), profiles = ref<Profile[]>([]), loading = ref(false), saving = ref(false), busy = ref('');
const blank = () => ({ id: '', name: '', clientID: '', clientSecret: '', parentId: 0, englishParentId: undefined as number | undefined, cdnKey: '', urlAuth: true });
const form = reactive(blank());
const activeName = computed(() => active.value === 'local' ? '本地存储' : profiles.value.find(p => p.id === active.value)?.name || active.value);
async function load() { loading.value = true; try { const data: any = await albumGet('storage/get'); active.value = data.active; profiles.value = data.profiles; } finally { loading.value = false; } }
function reset() { Object.assign(form, blank()); }
function edit(p: Profile) { reset(); Object.assign(form, { id: p.id, name: p.name, clientID: p.clientID, parentId: p.parentId, englishParentId: p.englishParentId ?? undefined, urlAuth: p.urlAuth }); }
async function save() { saving.value = true; try { const data: any = await albumPost('storage/save', form); form.id = data.id; form.clientSecret = ''; form.cdnKey = ''; await load(); Message.success('连接已保存，请验证后启用'); } finally { saving.value = false; } }
async function test(id: string) { busy.value = id; try { await albumPost('storage/test', { id }); await load(); Message.success('连接验证成功'); } finally { busy.value = ''; } }
async function activate(id: string) { await albumPost('storage/activate', { id }); await load(); Message.success('存储设置已生效'); }
onMounted(load);
</script>
<style scoped>.storage-settings{padding:20px}.actions{margin:20px 0}.connection{max-width:700px;margin-top:24px}</style>

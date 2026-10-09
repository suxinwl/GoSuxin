<template>
  <section class="runtime-plugin-page">
    <a-spin v-if="loading" />
    <a-alert v-else-if="error" type="error">{{ error }}<a-button size="small" @click="openAdmin">重试</a-button></a-alert>
    <iframe v-else-if="src" :src="src" :title="String(route.meta.title || '插件管理')" />
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { bootstrapRuntimePluginAdmin } from '@/utils/runtime-plugin';

const route = useRoute();
const loading = ref(false);
const error = ref('');
const src = ref('');
let generation = 0;

async function openAdmin() {
  const current = ++generation;
  const plugin = String(route.meta.runtimePlugin || '') || route.path.match(/^\/runtime-plugins\/([a-z][a-z0-9-]{0,63})\/?$/)?.[1];
  src.value = '';
  error.value = '';
  if (!plugin) { error.value = '插件管理地址无效'; return; }
  loading.value = true;
  const target = `/plugins/${plugin}/admin/${route.meta.runtimePlugin ? route.path.replace(/^\//, '') : ''}`;
  try {
    await bootstrapRuntimePluginAdmin(`/plugins/${plugin}/admin/`);
    if (current === generation) src.value = target;
  } catch (cause) {
    if (current === generation) error.value = cause instanceof Error ? cause.message : '无法打开插件管理，请确认登录和角色权限';
  } finally {
    if (current === generation) loading.value = false;
  }
}
watch(() => route.path, openAdmin, { immediate: true });
</script>

<style scoped>
.runtime-plugin-page { height: calc(100vh - 140px); min-height: 480px; padding: 16px; }
iframe { display: block; width: 100%; height: 100%; border: 0; }
</style>

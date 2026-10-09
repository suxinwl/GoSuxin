<template>
  <a-card v-if="state.items.length" title="图片上传记录" class="upload-record" data-testid="image-upload-status">
    <p>{{ state.title }} · 已选择 {{ state.items.length }} 张</p>
    <a-progress :percent="completed / state.items.length" :show-text="false" />
    <p role="status">{{ state.running ? '正在处理' : state.paused ? '已暂停' : '本次处理结束' }}：成功 {{ count('saved') }} 张，失败 {{ count('failed') }} 张，结果待确认 {{ count('unknown') }} 张，未发送 {{ count('pending') }} 张</p>
    <a-space style="margin-bottom: 12px">
      <a-button v-if="state.running" :disabled="state.pauseRequested" @click="$emit('pause')">{{ state.pauseRequested ? '等待当前文件结束' : '暂停后续上传' }}</a-button>
      <a-button v-if="state.paused && !state.running && count('pending')" type="primary" @click="$emit('resume')">继续未上传文件（{{ count('pending') }} 张）</a-button>
    </a-space>
    <p class="upload-note">上传期间保留逐张结果，批次结束或暂停后统一刷新图片列表。总进度按已处理文件数计算；单个文件上传到 100% 后，仍需等待服务器确认保存。</p>
    <a-alert v-if="state.error" type="error">{{ state.error }}</a-alert>
    <a-alert v-if="state.refreshError" type="warning">文件保存结果已记录，但列表刷新失败：{{ state.refreshError }}。请点击刷新查看，勿重复上传已成功文件。</a-alert>
    <a-alert v-if="count('unknown')" type="warning">部分请求断线或响应异常，可能已经保存。请先刷新图片列表核对，确认缺失后再选择对应文件上传。“继续未上传文件”不会重传这些结果待确认的文件。</a-alert>
    <div class="upload-files">
      <div v-for="(item, index) in state.items" :key="index" class="upload-row" :data-status="item.status">
        <div class="upload-row-heading"><span>{{ index + 1 }}. {{ item.name }}</span><span>{{ formatSize(item.size) }}</span></div>
        <div class="file-progress" role="progressbar" :aria-label="item.name + ' 上传进度'" :aria-valuenow="item.progressKnown || item.status === 'pending' ? percent(item) : undefined" aria-valuemin="0" aria-valuemax="100">
          <a-progress aria-hidden="true" :percent="percent(item) / 100" :show-text="false" :status="item.status === 'failed' || item.status === 'unknown' ? 'danger' : item.status === 'saved' ? 'success' : 'normal'" />
          <span>{{ item.progressKnown || item.status === 'pending' ? percent(item) + '%' : '等待进度' }}</span>
        </div>
        <div class="upload-row-detail"><span>{{ labels[item.status] }}<template v-if="item.error">：{{ item.error }}</template></span><span v-if="item.progressKnown">已传输 {{ formatSize(item.sent) }} / {{ formatSize(item.size) }}</span></div>
      </div>
    </div>
  </a-card>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { UploadItem } from './imageUpload';
const props = defineProps<{state: {title: string; running: boolean; paused: boolean; pauseRequested: boolean; items: UploadItem[]; error: string; refreshError: string}}>();
defineEmits<{(event:'pause'):void; (event:'resume'):void}>();
const labels = {pending:'等待上传', uploading:'正在上传', saving:'等待服务器保存', saved:'已保存', failed:'上传失败', unknown:'结果待确认'};
const percent = (item: UploadItem) => Math.floor(item.progress * 100);
const formatSize = (bytes: number) => bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 / 1024).toFixed(2)} MB`;
const count = (status: UploadItem['status']) => props.state.items.filter(i => i.status === status).length;
const completed = computed(() => count('saved') + count('failed') + count('unknown'));
</script>
<style scoped>
.upload-record { margin: 16px 0; }
.upload-files { max-height: 380px; overflow: auto; margin-top: 12px; }
.upload-row { padding: 12px 0; border-bottom: 1px solid var(--color-border-2); overflow-wrap: anywhere; }
.upload-row-heading, .upload-row-detail { display: flex; flex-wrap: wrap; gap: 8px 18px; justify-content: space-between; }
.upload-row-heading { color: var(--color-text-1); }
.upload-row-detail, .upload-note { font-size: 12px; color: var(--color-text-3); }
.file-progress { display: flex; align-items: center; gap: 12px; margin: 6px 0; }
.file-progress > span { min-width: 65px; text-align: right; }
.upload-row[data-status="failed"], .upload-row[data-status="unknown"] { color: rgb(var(--danger-6)); }
.upload-row[data-status="failed"] .upload-row-detail, .upload-row[data-status="unknown"] .upload-row-detail { color: inherit; }
</style>

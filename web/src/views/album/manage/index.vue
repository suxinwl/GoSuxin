<template>
  <div class="album-admin">
    <a-card :bordered="false">
      <template #title>Suxin 电子画册</template>
      <template #extra>
        <a-space>
          <a-button @click="openPublic">打开公开书架</a-button>
          <a-button type="primary" v-if="can('save')" @click="openEditor()"
            >新建画册</a-button
          >
        </a-space>
      </template>
      <a-space class="filters" wrap>
        <a-select
          :disabled="imageUpload.state.running"
          v-model="query.language"
          allow-clear
          placeholder="全部语言"
          style="width: 140px"
          @change="filter"
          ><a-option value="zh">中文</a-option
          ><a-option value="en">英文</a-option></a-select
        >
        <a-input-search
          :disabled="imageUpload.state.running"
          v-model="query.keyword"
          placeholder="搜索画册标题"
          style="width: 220px"
          @search="filter"
          @press-enter="filter"
        />
        <a-select
          :disabled="imageUpload.state.running"
          v-model="query.visibility"
          allow-clear
          placeholder="全部访问方式"
          style="width: 150px"
          @change="filter"
          ><a-option value="public">公开</a-option
          ><a-option value="private">私密</a-option></a-select
        >
        <a-select
          :disabled="imageUpload.state.running"
          v-model="query.categoryId"
          allow-clear
          placeholder="全部分类"
          style="width: 160px"
          @change="filter"
        >
          <a-option
            v-for="item in categories"
            :key="item.id"
            :value="Number(item.id)"
            >{{ item.name }}</a-option
          >
        </a-select>
        <a-select
          :disabled="imageUpload.state.running"
          v-model="query.status"
          allow-clear
          placeholder="全部状态"
          style="width: 150px"
          @change="filter"
        >
          <a-option value="draft">草稿</a-option
          ><a-option value="processing">转换中</a-option>
          <a-option value="ready">待发布</a-option
          ><a-option value="published">已发布</a-option>
          <a-option value="offline">已下架</a-option
          ><a-option value="failed">处理失败</a-option>
        </a-select>
        <a-button :disabled="imageUpload.state.running" @click="load">刷新</a-button>
      </a-space>
      <ImageUploadStatus :state="imageUpload.state" @pause="imageUpload.pause" @resume="imageUpload.resume" />
      <a-table
        :data="rows"
        :loading="loading"
        row-key="id"
        :pagination="pagination"
        @page-change="changePage"
      >
        <template #columns>
          <a-table-column title="封面" :width="80"
            ><template #cell="{ record }"
              ><a-avatar shape="square" :size="52"
                ><img
                  v-if="record.cover_url && !imageUpload.state.running" loading="lazy" decoding="async"
                  :src="record.cover_url" /></a-avatar></template
          ></a-table-column>
          <a-table-column title="画册"
            ><template #cell="{ record }"
              ><strong>{{ record.title }}</strong
              ><div class="muted"
                >{{ record.category }} · {{ record.page_count }} 页</div
              ></template
            ></a-table-column
          >
          <a-table-column title="语言" :width="80"
            ><template #cell="{ record }">{{
              record.language === 'en' ? '英文' : '中文'
            }}</template></a-table-column
          >
          <a-table-column title="可见性" data-index="visibility" :width="100"
            ><template #cell="{ record }"
              ><a-tag
                :color="record.visibility === 'public' ? 'blue' : 'gray'"
                >{{ record.visibility === 'public' ? '公开' : '私密' }}</a-tag
              ></template
            ></a-table-column
          >
          <a-table-column title="状态" data-index="status" :width="180"
            ><template #cell="{ record }"
              ><a-tag>{{
                record.status === 'processing'
                  ? processingText[record.processing_stage] || '处理中'
                  : statusText[record.status] || record.status
              }}</a-tag
              ><div v-if="record.failure_reason" class="failure">{{
                record.failure_reason
              }}</div></template
            ></a-table-column
          >
          <a-table-column title="操作" :width="390"
            ><template #cell="{ record }"
              ><a-space wrap>
                <a-button
                  size="small"
                  v-if="can('save')"
                  @click="openEditor(record)"
                  >编辑</a-button
                >
                <label
                  v-if="can('page/add')"
                  class="upload-button"
                  :class="{ disabled: record.status === 'processing' || imageUpload.state.running }"
                  >上传图片<input
                    type="file"
                    :disabled="record.status === 'processing' || imageUpload.state.running"
                    accept="image/jpeg,image/png,image/webp"
                    multiple
                    @change="uploadImages(record, $event)"
                /></label>
                <label
                  v-if="can('pdf/upload')"
                  class="upload-button"
                  :class="{ disabled: record.status === 'processing' || imageUpload.state.running }"
                  >上传 PDF<input
                    type="file"
                    :disabled="record.status === 'processing' || imageUpload.state.running"
                    accept="application/pdf"
                    @change="uploadPdf(record, $event)"
                /></label>
                <a-button
                  v-if="
                    can('pdf/retry') &&
                    record.status === 'failed' &&
                    record.source_type === 'pdf'
                  "
                  size="small"
                  @click="retryPdf(record)"
                  >重试转换</a-button
                >
                <a-button
                  v-if="can('publish')"
                  size="small"
                  :disabled="
                    imageUpload.state.running || !['ready', 'offline', 'published'].includes(
                      record.status
                    ) || record.page_count < 1
                  "
                  :status="record.status === 'published' ? 'warning' : 'normal'"
                  @click="togglePublish(record)"
                  >{{
                    record.status === 'published' ? '下架' : '发布'
                  }}</a-button
                >
                <a-button
                  v-if="can('share/create')"
                  size="small"
                  :disabled="record.status !== 'published'"
                  @click="dialog?.open(record)"
                  >创建分享</a-button
                >
                <a-button
                  v-if="can('page/list')"
                  :disabled="imageUpload.state.running"
                  size="small"
                  @click="
                    router.push({
                      path: '/albums-admin/images',
                      query: { albumId: record.id },
                    })
                  "
                  >管理图片</a-button
                >
                <a-button
                  v-if="can('share/list')"
                  size="small"
                  @click="
                    router.push({
                      path: '/albums-admin/shares',
                      query: { albumId: record.id },
                    })
                  "
                  >管理分享</a-button
                >
              </a-space></template
            ></a-table-column
          >
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:visible="editorVisible"
      :title="form.id ? '编辑画册' : '新建画册'"
      :on-before-ok="submit"
      :ok-loading="saving"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="画册语言" required
          ><a-select v-model="form.language"
            ><a-option value="zh">中文</a-option
            ><a-option value="en">英文</a-option></a-select
          ><template #help
            >标题、简介和上传内容请使用对应语言；英文站仅展示英文画册。更改语言不移动已有文件，新上传进入对应语言目录。</template
          ></a-form-item
        >
        <a-form-item label="标题" required
          ><a-input v-model="form.title"
        /></a-form-item>
        <a-form-item label="分类"
          ><a-select v-model="form.categoryId"
            ><a-option
              v-for="item in categories"
              :key="item.id"
              :value="Number(item.id)"
              >{{ item.name }}</a-option
            ></a-select
          ></a-form-item
        >
        <a-form-item label="简介"
          ><a-textarea
            v-model="form.description"
            :auto-size="{ minRows: 3, maxRows: 6 }"
        /></a-form-item>
        <a-form-item label="排序（数字越大越靠前）"
          ><a-input-number v-model="form.sortOrder" :precision="0"
        /></a-form-item>
        <a-form-item label="访问方式"
          ><a-radio-group v-model="form.visibility"
            ><a-radio value="public">公开书架</a-radio
            ><a-radio value="private">仅分享链接</a-radio></a-radio-group
          ></a-form-item
        >
      </a-form>
    </a-modal>
    <ShareDialog ref="dialog" />
  </div>
</template>

<script setup lang="ts">
  import { albumSite } from '@/utils/album-site';
  import { onMounted, onUnmounted, reactive, ref } from 'vue';
  import { useRouter } from 'vue-router';
  import { useAlbumAccess, ensureEditable } from '../shared';
  import { useImageUpload } from '../imageUpload';
  import ImageUploadStatus from '../ImageUploadStatus.vue';
  import ShareDialog from '../ShareDialog.vue';
  import { Message } from '@arco-design/web-vue';
  import {
    albumGet,
    getAlbums,
    publishAlbum,
    saveAlbum,
    uploadAlbumPdf,
    retryAlbumPdf,
    type AlbumRecord,
  } from '@/api/album';

  const imageUpload = useImageUpload();
  const categories = ref<any[]>([]);
  const router = useRouter(),
    { can } = useAlbumAccess();
  const dialog = ref<InstanceType<typeof ShareDialog>>();
  const statusText: Record<string, string> = {
    draft: '草稿',
    processing: '转换中',
    ready: '待发布',
    published: '已发布',
    offline: '已下架',
    failed: '处理失败',
  };
  const rows = ref<AlbumRecord[]>([]);
  const loading = ref(false);
  const saving = ref(false);
  const editorVisible = ref(false);
  const query = reactive({
    page: 1,
    pageSize: 20,
    categoryId: undefined as number | undefined,
    status: '',
    keyword: '',
    visibility: '',
    language: '',
  });
  const pagination = reactive({
    current: 1,
    pageSize: 20,
    total: 0,
    showTotal: true,
  });
  const form = reactive({
    id: 0,
    title: '',
    language: 'zh',
    description: '',
    categoryId: undefined as number | undefined,
    sortOrder: 0,
    visibility: 'public' as 'public' | 'private',
  });

  let loadSequence = 0;
  async function load() {
    const sequence = ++loadSequence;
    loading.value = true;
    try {
      const data: any = await getAlbums({ ...query });
      if (sequence !== loadSequence) return;
      rows.value = data.items || [];
      pagination.total = data.total || 0;
      imageUpload.state.refreshError = '';
    } finally {
      if (sequence === loadSequence) loading.value = false;
    }
  }
  function changePage(page: number) {
    if (imageUpload.state.running) return;
    query.page = page;
    pagination.current = page;
    load();
  }
  function filter() {
    if (imageUpload.state.running) return;
    query.page = 1;
    pagination.current = 1;
    load();
  }
  function openEditor(record?: AlbumRecord) {
    Object.assign(form, {
      id: record?.id || 0,
      title: record?.title || '',
      language: record?.language || 'zh',
      description: record?.description || '',
      categoryId:
        Number(record?.category_id || categories.value[0]?.id) || undefined,
      sortOrder: Number(record?.sort_order || 0),
      visibility: record?.visibility || 'private',
    });
    editorVisible.value = true;
  }
  async function submit() {
    if (!form.title.trim() || !form.categoryId) {
      Message.warning('请填写标题并选择分类');
      return false;
    }
    saving.value = true;
    try {
      await saveAlbum({ ...form });
      await load();
      return true;
    } catch {
      return false;
    } finally {
      saving.value = false;
    }
  }
  async function togglePublish(record: AlbumRecord) {
    await publishAlbum(record.id, record.status !== 'published');
    Message.success(record.status === 'published' ? '已下架' : '已发布');
    await load();
  }
  async function uploadImages(record: AlbumRecord, event: Event) {
    const input = event.target as HTMLInputElement;
    const files = Array.from(input.files || []);
    input.value = '';
    await imageUpload.run(record, files, can, load);
  }
  async function uploadPdf(record: AlbumRecord, event: Event) {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    try {
      if (file && (await ensureEditable(record, can))) {
        await uploadAlbumPdf(record.id, { file });
        Message.success('PDF 已提交后台转换');
        await load();
      }
    } finally {
      input.value = '';
    }
  }
  function openPublic() {
    window.open(albumSite('zh'), '_blank');
  }
  async function retryPdf(record: AlbumRecord) {
    await retryAlbumPdf(record.id);
    Message.success('已重新提交转换');
    await load();
  }
  let pollTimer: ReturnType<typeof setInterval>;
  onMounted(async () => {
    categories.value = ((await albumGet('category/list')) as any) || [];
    load();
    pollTimer = setInterval(() => {
      if (
        !loading.value && !imageUpload.state.running &&
        rows.value.some((row) => row.status === 'processing')
      )
        load();
    }, 3000);
  });
  onUnmounted(() => clearInterval(pollTimer));
const processingText: Record<string, string> = {source_upload: '原文件上传', converting: 'PDF 转换', page_upload: '页面上传'};
</script>

<style scoped>
  .failure {
    margin-top: 6px;
    color: rgb(var(--danger-6));
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .upload-button.disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .album-admin {
    padding: 20px;
  }
  .filters {
    margin-bottom: 18px;
  }
  .muted {
    margin-top: 6px;
    color: var(--color-text-3);
    font-size: 12px;
  }
  .upload-button {
    height: 28px;
    padding: 0 11px;
    border: 1px solid var(--color-neutral-3);
    border-radius: 2px;
    display: inline-flex;
    align-items: center;
    cursor: pointer;
  }
  .upload-button:hover {
    border-color: rgb(var(--primary-6));
    color: rgb(var(--primary-6));
  }
  .upload-button input {
    display: none;
  }
</style>

<template>
  <div class="center-page">
    <a-card title="分类管理" :bordered="false">
      <template #extra
        ><a-button v-if="can('category/save')" type="primary" @click="edit()"
          >新增分类</a-button
        ></template
      >
      <a-alert style="margin-bottom: 16px"
        >分类由两个站点共用，中英文标题、简介和封面分别设置。未设置封面时，前台使用本站该分类的第一本公开画册封面。</a-alert
      >
      <a-table :data="rows" :loading="loading" row-key="id" :pagination="false">
        <template #columns>
          <a-table-column title="中文封面"
            ><template #cell="{ record }"
              ><a-image
                v-if="record.cover_url"
                :src="record.cover_url"
                width="64"
                height="80"
              /><span v-else>自动</span></template
            ></a-table-column
          >
          <a-table-column title="英文封面"
            ><template #cell="{ record }"
              ><a-image
                v-if="record.english_cover_url"
                :src="record.english_cover_url"
                width="64"
                height="80"
              /><span v-else>自动</span></template
            ></a-table-column
          >
          <a-table-column title="中文名称" data-index="name" />
          <a-table-column title="英文标题" data-index="english_title" />
          <a-table-column title="排序" data-index="sort_order" />
          <a-table-column title="操作"
            ><template #cell="{ record }"
              ><a-space>
                <a-button
                  v-if="can('category/save')"
                  size="small"
                  @click="edit(record)"
                  >编辑</a-button
                >
                <a-button
                  v-if="can('category/cover')"
                  size="small"
                  @click="cover = record"
                  >设置封面</a-button
                >
                <a-button
                  v-if="can('category/delete')"
                  size="small"
                  status="danger"
                  @click="remove(record)"
                  >删除</a-button
                >
              </a-space></template
            ></a-table-column
          >
        </template>
      </a-table>
    </a-card>
    <a-modal
      v-model:visible="visible"
      :title="form.id ? '编辑分类' : '新增分类'"
      :on-before-ok="save"
    >
      <a-form :model="form" layout="vertical">
        <a-form-item label="中文名称" required
          ><a-input v-model="form.name" :max-length="80"
        /></a-form-item>
        <a-form-item label="英文标题"
          ><a-input v-model="form.englishTitle" :max-length="120"
        /></a-form-item>
        <a-form-item label="中文简介"
          ><a-textarea v-model="form.description" :max-length="500"
        /></a-form-item>
        <a-form-item label="英文简介"
          ><a-textarea v-model="form.englishDescription" :max-length="500"
        /></a-form-item>
        <a-form-item label="排序（数字越小越靠前）"
          ><a-input-number v-model="form.sortOrder" :precision="0"
        /></a-form-item>
        <p>保存后可通过列表中的“设置封面”上传、替换或移除图片。</p>
      </a-form>
    </a-modal>
    <a-modal
      :visible="!!cover"
      title="分类封面"
      :footer="false"
      :mask-closable="!uploading"
      :closable="!uploading"
      @cancel="cover = null"
    >
      <div v-for="lang in languages" :key="lang.value" class="cover-editor">
        <strong>{{ lang.label }}封面</strong>
        <a-image
          v-if="cover?.[lang.field]"
          :src="cover[lang.field]"
          width="100"
        />
        <span v-else>未设置，将使用该语言分类中的首本画册封面</span>
        <input
          type="file"
          accept="image/jpeg,image/png,image/webp"
          :disabled="uploading"
          @change="uploadCover(lang.value, $event)"
        />
        <a-button
          v-if="cover?.[lang.field]"
          :disabled="uploading"
          status="danger"
          @click="removeCover(lang.value)"
          >移除{{ lang.label }}封面</a-button
        >
      </div>
      <p
        >支持 JPG、PNG、WEBP，最大 20
        MB。上传后立即生效；分类封面是公开图片。</p
      >
    </a-modal>
  </div>
</template>
<script setup lang="ts">
  import { onMounted, reactive, ref } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { albumGet, albumPost, uploadCategoryCover } from '@/api/album';
  import { useAlbumAccess, confirmAction } from '../shared';
  const { can } = useAlbumAccess();
  const rows = ref<any[]>([]),
    loading = ref(false),
    visible = ref(false),
    cover = ref<any>(null),
    uploading = ref(false);
  const languages = [
    { value: 'zh', label: '中文', field: 'cover_url' },
    { value: 'en', label: '英文', field: 'english_cover_url' },
  ];
  const form = reactive({
    id: 0,
    name: '',
    englishTitle: '',
    description: '',
    englishDescription: '',
    sortOrder: 0,
  });
  async function load() {
    loading.value = true;
    try {
      rows.value = ((await albumGet('category/list')) as any) || [];
      if (cover.value)
        cover.value = rows.value.find((r) => r.id === cover.value.id) || null;
    } finally {
      loading.value = false;
    }
  }
  function edit(row?: any) {
    Object.assign(form, {
      id: row?.id || 0,
      name: row?.name || '',
      englishTitle: row?.english_title || '',
      description: row?.description || '',
      englishDescription: row?.english_description || '',
      sortOrder: Number(row?.sort_order || 0),
    });
    visible.value = true;
  }
  async function save() {
    if (!form.name.trim()) {
      Message.warning('请输入分类名称');
      return false;
    }
    try {
      await albumPost('category/save', { ...form });
      await load();
      return true;
    } catch {
      return false;
    }
  }
  async function uploadCover(language: string, event: Event) {
    const input = event.target as HTMLInputElement,
      file = input.files?.[0];
    if (!file) return;
    if (file.size > 20 * 1024 * 1024) {
      Message.warning('封面不能超过 20 MB');
      input.value = '';
      return;
    }
    uploading.value = true;
    try {
      await uploadCategoryCover(cover.value.id, language, file);
      await load();
      Message.success('分类封面已更新');
    } finally {
      uploading.value = false;
      input.value = '';
    }
  }
  async function removeCover(language: string) {
    if (
      !(await confirmAction(
        '移除分类封面',
        '移除后前台自动使用该分类的画册封面。云盘旧封面无引用后将移入回收站。'
      ))
    )
      return;
    uploading.value = true;
    try {
      await albumPost('category/cover', {
        id: cover.value.id,
        language,
        remove: true,
      });
      await load();
    } finally {
      uploading.value = false;
    }
  }
  async function remove(row: any) {
    if (
      await confirmAction(
        '删除分类',
        `确认删除“${row.name}”？已有画册的分类不能删除。`
      )
    ) {
      await albumPost('category/delete', { id: row.id });
      await load();
    }
  }
  onMounted(load);
</script>
<style scoped>
  .center-page {
    padding: 20px;
  }
  .cover-editor {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
    margin-bottom: 24px;
  }
</style>

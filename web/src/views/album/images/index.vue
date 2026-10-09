<template
  ><div class="center-page"
    ><a-card title="图片管理" :bordered="false"
      ><a-space wrap class="toolbar"
        ><a-select
          v-model="albumId"
          :disabled="busy"
          allow-search
          placeholder="选择画册"
          style="width: 300px"
          @change="load"
          ><a-option v-for="a in albums" :key="a.id" :value="a.id">{{
            a.title
          }}</a-option></a-select
        ><a-button :disabled="busy" @click="load">刷新</a-button
        ><a-button
          v-if="can('page/add')"
          type="primary"
          :disabled="!active || busy"
          @click="chooseUpload"
          >批量上传</a-button
        ><a-button
          v-if="can('page/delete')"
          status="danger"
          :disabled="!selected.length || busy"
          @click="remove"
          >删除选中</a-button
        ><span v-if="active"
          >{{ pages.length }} 页 ·
          {{
            active.status === 'published'
              ? '已发布'
              : active.status === 'processing'
              ? '转换中'
              : '待发布'
          }}</span
        ></a-space
      >
      <a-alert v-if="active?.status === 'published'" style="margin-bottom: 16px"
        >查看图片不会影响线上阅读；修改前会提示下架，完成后请到画册管理重新发布。</a-alert
      >
      <a-alert v-if="active?.cloud_cleanup?.pending" type="warning" style="margin-bottom:16px">
        页面已更新，{{ active.cloud_cleanup.pending }} 个云盘文件待同步删除，后台会自动重试。
        <div v-if="active.cloud_cleanup.lastError">{{ active.cloud_cleanup.lastError }}</div>
        <a-button v-if="can('page/cleanup/retry')" size="mini" :loading="busy" @click="retryCleanup">立即重试</a-button>
      </a-alert>
      <p v-if="active" class="cleanup-note">删除或替换后，不再引用的云盘图片会同步移入云盘回收站；仍被其他页面使用的文件保留。</p>
      <a-alert v-if="orderMessage" :type="orderWarning ? 'warning' : 'info'" class="order-status" role="status">{{ orderMessage }}</a-alert>
      <ImageUploadStatus :state="imageUpload.state" @pause="imageUpload.pause" @resume="imageUpload.resume" />
      <a-spin :loading="busy && !imageUpload.state.running" style="width: 100%"
        ><a-empty
          v-if="!pages.length"
          description="暂无页面，请选择画册或上传图片"
        /><div class="page-grid"
          ><article
            v-for="(page, index) in pages"
            :key="page.id"
            :data-page-id="page.id"
            :draggable="can('page/reorder') && !busy && !orderUncertain"
            @dragstart="startDrag(page.id, $event)"
            @dragend="dragged = 0"
            @dragover.prevent
            @drop.prevent="drop(page.id)"
            ><a-checkbox
              :model-value="selected.includes(page.id)"
              @change="toggleSelection(page.id, $event)"
              class="page-select"
              >选择</a-checkbox
            ><a-image
              v-if="!imageUpload.state.running"
              :draggable="false"
              loading="lazy"
              decoding="async"
              :src="page.image_url"
              :title="'第 ' + (index + 1) + ' 页'"
              width="100%"
              height="220"
              fit="contain"
            /><div v-else class="preview-paused">上传完成后加载预览</div><div class="page-title"
              >第 {{ index + 1 }} 页
              <a-tag v-if="page.is_cover" color="blue">封面</a-tag></div
            ><a-space
              ><a-button
                v-if="can('page/cover')"
                size="mini"
                :disabled="page.is_cover || busy"
                @click="setCover(page)"
                >设为封面</a-button
              ><a-button
                v-if="can('page/replace')"
                size="mini"
                :disabled="busy"
                @click="chooseReplace(page)"
                >替换</a-button
              ></a-space
            ><small v-if="can('page/reorder')">拖动画片调整顺序</small></article
          ></div
        ></a-spin
      ></a-card
    >
    <input
      ref="uploadInput"
      hidden
      type="file"
      accept="image/jpeg,image/png,image/webp"
      multiple
      @change="upload" /><input
      ref="replaceInput"
      hidden
      type="file"
      accept="image/jpeg,image/png,image/webp"
      @change="replace" /></div
></template>
<script setup lang="ts">
  import { computed, ref, onMounted, onUnmounted, watch } from 'vue';
  import { useImageUpload } from '../imageUpload';
  import ImageUploadStatus from '../ImageUploadStatus.vue';
  import { useRoute } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import {
    albumGet,
    albumPost,
    replaceAlbumPage,
  } from '@/api/album';
  import {
    allAlbums,
    useAlbumAccess,
    ensureEditable,
    confirmAction,
  } from '../shared';
  const route = useRoute(),
    { can } = useAlbumAccess();
  const albums = ref<any[]>([]),
    albumId = ref<number>(),
    active = ref<any>(null),
    pages = ref<any[]>([]),
    selected = ref<number[]>([]),
    operationBusy = ref(false),
    dragged = ref(0),
    uploadInput = ref<HTMLInputElement>(),
    replaceInput = ref<HTMLInputElement>();
  const imageUpload = useImageUpload();
  const busy = computed(() => operationBusy.value || imageUpload.state.running);
  const orderMessage = ref(''), orderWarning = ref(false), orderUncertain = ref(false);
  let replacing = 0;
  let loadSequence = 0;
  async function refreshPages() {
    const id = albumId.value, sequence = ++loadSequence;
    if (!id) { active.value = null; pages.value = []; return; }
    const data: any = await albumGet('page/list', { albumId: id });
    if (sequence !== loadSequence || id !== albumId.value) return;
    active.value = data.album; pages.value = data.pages || [];
    selected.value = selected.value.filter(id => pages.value.some(page => Number(page.id) === id));
  }
  async function load() {
    const checkingOrder = orderUncertain.value;
    operationBusy.value = true;
    try { await refreshPages(); imageUpload.state.refreshError = ''; orderUncertain.value = false; if (checkingOrder) { orderWarning.value = false; orderMessage.value = '已同步服务器当前顺序，可继续排序'; } }
    finally { operationBusy.value = false; }
  }
  async function act(action: () => Promise<unknown>) {
    if (!active.value || busy.value) return;
    operationBusy.value = true;
    try {
      if (!(await ensureEditable(active.value, can))) return;
      await action();
      await load();
    } finally {
      operationBusy.value = false;
    }
  }
  function chooseUpload() {
    uploadInput.value?.click();
  }
  function chooseReplace(page: any) {
    replacing = Number(page.id);
    replaceInput.value?.click();
  }
  async function upload(event: Event) {
    const input = event.target as HTMLInputElement;
    const files = Array.from(input.files || []);
    input.value = '';
    if (!active.value || busy.value) return;
    const id = Number(active.value.id);
    await imageUpload.run(active.value, files, can, async () => {
      if (Number(albumId.value) === id) await refreshPages();
    });
  }
  async function replace(event: Event) {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file)
      await act(() => replaceAlbumPage(active.value.id, replacing, file));
    input.value = '';
  }
  async function retryCleanup() {
    if (!active.value || busy.value) return;
    operationBusy.value = true;
    try { await albumPost('page/cleanup/retry', { albumId: active.value.id }); await load(); if (!active.value?.cloud_cleanup?.pending) Message.success('云盘文件同步删除完成'); }
    finally { operationBusy.value = false; }
  }
  let cleanupTimer: ReturnType<typeof setInterval>;
  onMounted(() => { cleanupTimer = setInterval(async () => {
    if (busy.value || !active.value?.cloud_cleanup?.pending) return;
    const id = active.value.id;
    try { const data: any = await albumGet('page/list', { albumId: id }); if (active.value?.id === id) active.value.cloud_cleanup = data.album.cloud_cleanup; } catch { /* Retry on next poll. */ }
  }, 10000); });
  onUnmounted(() => clearInterval(cleanupTimer));
  async function setCover(page: any) {
    await act(() =>
      albumPost('page/cover', { albumId: active.value.id, pageId: page.id })
    );
  }
  async function remove() {
    const ids = [...selected.value];
    if (!(await confirmAction('删除页面', `确认删除选中的 ${ids.length} 页？不再引用的云盘图片将同步移入回收站。`)))
      return;
    await act(() =>
      albumPost('page/delete', { albumId: active.value.id, ids })
    );
  }
  function startDrag(id: number, event: DragEvent) {
    if (busy.value || orderUncertain.value || !can('page/reorder')) { event.preventDefault(); return; }
    dragged.value = Number(id);
    if (event.dataTransfer) { event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', String(id)); }
  }
  async function drop(target: number) {
    const source = dragged.value;
    dragged.value = 0;
    if (!active.value || busy.value || orderUncertain.value || !can('page/reorder') || source === Number(target)) return;
    const id = Number(active.value.id), record = active.value;
    const from = pages.value.findIndex(page => Number(page.id) === source);
    const to = pages.value.findIndex(page => Number(page.id) === Number(target));
    if (from < 0 || to < 0) return;
    operationBusy.value = true;
    const previous = [...pages.value];
    let moved = false;
    try {
      if (!(await ensureEditable(record, can)) || Number(albumId.value) !== id) return;
      ++loadSequence; // An earlier read must not undo this local move.
      const reordered = [...previous];
      reordered.splice(to, 0, reordered.splice(from, 1)[0]);
      pages.value = reordered.map((page, index) => ({ ...page, page_no: index + 1 }));
      moved = true;
      orderWarning.value = false; orderMessage.value = '正在保存图片顺序…';
      await albumPost('page/reorder', { albumId: id, ids: pages.value.map(page => Number(page.id)) });
      if (Number(albumId.value) === id) orderMessage.value = '图片顺序已保存';
    } catch (e) {
      if (Number(albumId.value) !== id) return;
      orderWarning.value = true;
      const detail = e instanceof Error ? e.message : '请求失败';
      if (moved) pages.value = previous;
      orderMessage.value = `排序请求未能确认：${detail}。正在核对服务器顺序…`;
      try {
        await refreshPages();
        if (Number(albumId.value) === id) orderMessage.value = `排序请求未能确认：${detail}。已重新读取服务器当前顺序。`;
      } catch {
        if (Number(albumId.value) === id) {
          orderUncertain.value = true;
          orderMessage.value = `排序保存结果待确认：${detail}。请点击刷新核对后再排序。`;
        }
      }
    } finally { operationBusy.value = false; }
  }
  watch(
    () => route.query.albumId,
    async (value) => {
      if (value) {
        orderMessage.value = ''; orderUncertain.value = false;
        albumId.value = Number(value);
        await load();
      }
    }
  );
  function toggleSelection(id: number, checked: unknown) { selected.value = checked ? [...selected.value, id] : selected.value.filter(value => value !== id); }
  onMounted(async () => {
    albums.value = await allAlbums();
    albumId.value =
      Number(route.query.albumId) || Number(albums.value[0]?.id) || undefined;
    await load();
  });</script
><style scoped>
  .order-status { margin-bottom: 16px; }
  .preview-paused { height: 220px; display:flex; align-items:center; justify-content:center; color:var(--color-text-3); }
  .cleanup-note { color: var(--color-text-3); line-height: 1.7; }
  .center-page {
    padding: 20px;
  }
  .toolbar {
    margin-bottom: 18px;
  }
  .page-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
    gap: 18px;
  }
  .page-grid article {
    position: relative;
    padding: 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
    background: var(--color-bg-2);
  }
  .page-select {
    display: block;
    margin-bottom: 10px;
  }
  .page-title {
    margin: 10px 0;
  }
  .page-grid small {
    display: block;
    margin-top: 10px;
    color: var(--color-text-3);
  }
</style>

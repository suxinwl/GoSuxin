<template
  ><div class="center-page"
    ><a-card title="分享管理" :bordered="false"
      ><a-space class="toolbar"
        ><a-select
          v-model="albumId"
          placeholder="选择画册"
          allow-search
          style="width: 320px"
          @change="load"
          ><a-option v-for="a in albums" :key="a.id" :value="a.id">{{
            a.title
          }}</a-option></a-select
        ><a-button @click="load">刷新</a-button
        ><a-button
          v-if="can('share/create')"
          type="primary"
          :disabled="!active || active.status !== 'published'"
          @click="dialog?.open(active)"
          >创建分享</a-button
        ></a-space
      >
      <a-alert
        v-if="active && active.status !== 'published'"
        style="margin-bottom: 16px"
        >画册未发布，访客暂时无法通过分享阅读。</a-alert
      >
      <a-table :data="rows" row-key="id" :loading="loading"
        ><template #columns>
          <a-table-column title="链接"
            ><template #cell="{ record }"
              ><a-link
                :href="shareURL(record.share_key, active?.language)"
                target="_blank"
                >打开分享</a-link
              ></template
            ></a-table-column
          >
          <a-table-column title="密码"
            ><template #cell="{ record }">{{
              record.has_password ? '已设置' : '免密'
            }}</template></a-table-column
          >
          <a-table-column title="有效期"
            ><template #cell="{ record }">{{
              record.expires_at || '永久有效'
            }}</template></a-table-column
          >
          <a-table-column title="状态"
            ><template #cell="{ record }">{{
              !Number(record.enabled)
                ? '已停用'
                : record.expires_at &&
                  dayjs(record.expires_at).isBefore(dayjs())
                ? '已过期'
                : '已启用'
            }}</template></a-table-column
          >
          <a-table-column title="访问次数" data-index="visit_count" />
          <a-table-column title="操作"
            ><template #cell="{ record }"
              ><a-space
                ><a-button size="small" @click="copy(record)">复制链接</a-button
                ><a-button
                  v-if="can('share/update')"
                  size="small"
                  @click="dialog?.open(active, record)"
                  >修改设置</a-button
                ><a-button
                  v-if="can('share/update')"
                  size="small"
                  @click="toggle(record)"
                  >{{ Number(record.enabled) ? '停用' : '启用' }}</a-button
                ></a-space
              ></template
            ></a-table-column
          >
        </template></a-table
      ></a-card
    ><ShareDialog ref="dialog" @saved="load" /></div
></template>
<script setup lang="ts">
  import { ref, computed, onMounted, watch } from 'vue';
  import { useRoute } from 'vue-router';
  import dayjs from 'dayjs';
  import { Message } from '@arco-design/web-vue';
  import { albumGet, albumPost } from '@/api/album';
  import {
    allAlbums,
    shareURL,
    useAlbumAccess,
    confirmAction,
  } from '../shared';
  import ShareDialog from '../ShareDialog.vue';
  const route = useRoute(),
    { can } = useAlbumAccess();
  const albums = ref<any[]>([]),
    albumId = ref<number>(),
    rows = ref<any[]>([]),
    loading = ref(false),
    dialog = ref<InstanceType<typeof ShareDialog>>();
  const active = computed(() =>
    albums.value.find((a) => Number(a.id) === Number(albumId.value))
  );
  async function load() {
    if (!albumId.value) {
      rows.value = [];
      return;
    }
    loading.value = true;
    try {
      rows.value =
        ((await albumGet('share/list', { albumId: albumId.value })) as any) ||
        [];
    } finally {
      loading.value = false;
    }
  }
  async function copy(row: any) {
    try {
      await navigator.clipboard.writeText(
        shareURL(row.share_key, active.value?.language)
      );
      Message.success('已复制链接');
    } catch {
      Message.info(shareURL(row.share_key, active.value?.language));
    }
  }
  async function toggle(row: any) {
    if (
      !Number(row.enabled) &&
      row.expires_at &&
      dayjs(row.expires_at).isBefore(dayjs())
    ) {
      dialog.value?.open(active.value, row);
      return;
    }
    if (
      !(await confirmAction(
        Number(row.enabled) ? '停用分享' : '启用分享',
        '保存后，先前签发的阅读凭证会失效。'
      ))
    )
      return;
    await albumPost('share/update', {
      albumId: albumId.value,
      id: row.id,
      passwordMode: 'keep',
      permanent: !row.expires_at,
      expiresAt: row.expires_at,
      enabled: !Number(row.enabled),
    });
    await load();
  }
  watch(
    () => route.query.albumId,
    async (value) => {
      if (value) {
        albumId.value = Number(value);
        await load();
      }
    }
  );
  onMounted(async () => {
    albums.value = await allAlbums();
    albumId.value =
      Number(route.query.albumId) || Number(albums.value[0]?.id) || undefined;
    await load();
  });</script
><style scoped>
  .center-page {
    padding: 20px;
  }
  .toolbar {
    margin-bottom: 18px;
  }
</style>

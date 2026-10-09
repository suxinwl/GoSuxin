<template>
  <a-modal
    v-model:visible="visible"
    :title="editing ? '修改分享' : '创建分享'"
    :on-before-ok="submit"
    @cancel="visible = false"
  >
    <a-alert style="margin-bottom: 16px"
      >停用、改密或下架后本站立即停止授权。开启 URL 鉴权的云盘链接最长有效 60
      秒；关闭鉴权的直链可能长期有效，本站无法撤回。已下载内容和已开始的传输无法追回。</a-alert
    >
    <a-form :model="form" layout="vertical">
      <a-form-item label="画册">{{ album?.title }}</a-form-item>
      <a-form-item label="访问密码"
        ><a-select v-model="form.passwordMode"
          ><a-option v-if="editing" value="keep">保留当前密码设置</a-option
          ><a-option value="random">自动生成随机密码</a-option
          ><a-option value="custom">自定义密码</a-option
          ><a-option value="none">免密访问</a-option></a-select
        ></a-form-item
      >
      <a-form-item v-if="form.passwordMode === 'custom'" label="密码"
        ><a-input-password
          v-model="form.password"
          placeholder="4至72字节"
          autocomplete="new-password"
      /></a-form-item>
      <a-form-item label="永久有效"
        ><a-switch v-model="form.permanent"
      /></a-form-item>
      <a-form-item v-if="!form.permanent" label="有效期至"
        ><a-date-picker
          v-model="form.expiresAt"
          show-time
          value-format="YYYY-MM-DD HH:mm:ss"
          style="width: 100%"
      /></a-form-item>
      <a-form-item v-if="editing" label="启用分享"
        ><a-switch v-model="form.enabled"
      /></a-form-item>
    </a-form>
  </a-modal>
  <a-modal v-model:visible="resultVisible" title="分享已保存" :footer="false">
    <a-alert>新密码只在此处显示，请复制并妥善保存。</a-alert>
    <p class="share-result">{{ resultText }}</p
    ><a-button type="primary" @click="copy">复制链接与密码</a-button>
  </a-modal>
</template>
<script setup lang="ts">
  import { reactive, ref } from 'vue';
  import dayjs from 'dayjs';
  import { Message } from '@arco-design/web-vue';
  import { albumPost } from '@/api/album';
  import { shareURL } from './shared';
  const emit = defineEmits(['saved']);
  const visible = ref(false),
    editing = ref(false),
    album = ref<any>(null),
    resultVisible = ref(false),
    resultText = ref('');
  const form = reactive({
    id: 0,
    albumId: 0,
    passwordMode: 'random',
    password: '',
    permanent: false,
    expiresAt: '',
    enabled: true,
  });
  let key = '';
  function open(record: any, share?: any) {
    album.value = record;
    editing.value = !!share;
    key = share?.share_key || '';
    Object.assign(form, {
      id: share?.id || 0,
      albumId: record.id,
      passwordMode: share ? 'keep' : 'random',
      password: '',
      permanent: share ? !share.expires_at : false,
      expiresAt:
        share?.expires_at ||
        dayjs().add(7, 'day').format('YYYY-MM-DD HH:mm:ss'),
      enabled: share ? !!Number(share.enabled) : true,
    });
    visible.value = true;
  }
  async function submit() {
    if (!form.permanent && !form.expiresAt) {
      Message.warning('请选择到期时间');
      return false;
    }
    if (
      form.passwordMode === 'custom' &&
      (new TextEncoder().encode(form.password).length < 4 ||
        new TextEncoder().encode(form.password).length > 72)
    ) {
      Message.warning('密码须为4至72字节');
      return false;
    }
    try {
      const data: any = await albumPost(
        editing.value ? 'share/update' : 'share/create',
        {
          ...form,
          password: form.passwordMode === 'custom' ? form.password : '',
          expiresAt: form.permanent ? null : form.expiresAt,
        }
      );
      key = data.key || key;
      resultText.value =
        shareURL(key, album.value?.language) +
        (data.password
          ? `\n密码：${data.password}`
          : form.passwordMode === 'none'
          ? '\n免密访问'
          : '\n密码设置保持不变') +
        (form.permanent ? '\n永久有效' : `\n有效期至：${form.expiresAt}`);
      visible.value = false;
      resultVisible.value = true;
      emit('saved');
      return true;
    } catch {
      return false;
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(resultText.value);
      Message.success('已复制');
    } catch {
      Message.info('请选中上方内容手动复制');
    }
  }
  defineExpose({ open });
</script>
<style scoped>
  .share-result {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.8;
  }
</style>

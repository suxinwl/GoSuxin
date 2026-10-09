import { ref, onMounted } from 'vue';
import { Modal, Message } from '@arco-design/web-vue';
import { albumGet, publishAlbum } from '@/api/album';
import { albumSite, loadAlbumSites } from '@/utils/album-site';
export function useAlbumAccess() {
  const permissions = ref<string[]>([]);
  onMounted(async () => {
    await loadAlbumSites();
    permissions.value = ((await albumGet('permissions')) as any) || [];
  });
  const can = (action: string) =>
    permissions.value.includes('*') ||
    permissions.value.includes(`/admin/album/${action}`);
  return { can };
}
export const confirmAction = (title: string, content: string) =>
  new Promise<boolean>((resolve) =>
    Modal.confirm({
      title,
      content,
      onOk: () => resolve(true),
      onCancel: () => resolve(false),
    })
  );
export async function ensureEditable(
  album: any,
  can: (action: string) => boolean
) {
  if (album.status === 'processing') {
    Message.warning('PDF 正在转换，请稍后再试');
    return false;
  }
  if (album.status !== 'published') return true;
  if (!can('publish')) {
    Message.warning('请联系有发布权限的管理员先下架画册');
    return false;
  }
  if (
    !(await confirmAction(
      '下架后编辑',
      '此操作会暂停公开及分享阅读。确认下架并编辑图片？完成后需手动重新发布。'
    ))
  )
    return false;
  await publishAlbum(album.id, false);
  album.status = 'offline';
  return true;
}
export async function allAlbums() {
  const result: any[] = [];
  for (let page = 1; ; page++) {
    const data: any = await albumGet('list', { page, pageSize: 100 });
    const batch = data.items || [];
    result.push(...batch);
    if (!batch.length || result.length >= data.total) return result;
  }
}
export function shareURL(key: string, language = 'zh') {
  return `${albumSite(language)}share/${key}`;
}

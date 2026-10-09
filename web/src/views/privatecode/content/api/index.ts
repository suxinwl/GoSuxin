import { defHttp } from '@/utils/http';
import type { AxiosResponse } from 'axios';

export interface Category { id: number; name: string; remark: string; weigh: number }
export interface PackageRecord {
  id: number; cid: number; title: string; name: string; des: string;
  content: string; author: string; version: string; status: number;
  download: number; can_edit: boolean; updatetime?: string;
}
export interface Release {
  id: number; version: string; filename: string; sha256: string;
  size: number; note: string; download: number; createtime: string;
}
export const repoGet = <T = any>(path: string, params: Record<string, unknown> = {}) =>
  defHttp.get<T>({ url: `/privatecode/${path}`, params });
export const repoPost = (path: string, params: object) =>
  defHttp.post({ url: `/privatecode/${path}`, params });

export async function uploadRelease(id: number, version: string, note: string, publish: boolean, file: File) {
  const root = import.meta.env.VITE_APP_ENV === 'production' ? window.globalConfig.Root_url : window.globalConfig.Root_url_dev;
  const result = await defHttp.uploadFile({ url: `${root}/privatecode/release/upload`, timeout: 600000 }, { file, data: { id, version, note, publish } });
  if (!result || result.code !== 0) throw new Error(result?.message || '源码包上传失败');
  return result.data;
}
export async function downloadRelease(release: Release) {
  const result = await defHttp.get<AxiosResponse<Blob>>(
    { url: '/privatecode/release/download', params: { id: release.id }, responseType: 'blob', timeout: 600000 },
    { isReturnNativeResponse: true, retryRequest: { isOpenRetry: false, count: 0, waitTime: 0 } }
  );
  if (result.data.type.includes('json')) {
    const body = JSON.parse(await result.data.text());
    throw new Error(body.message || '下载失败');
  }
  const url = URL.createObjectURL(result.data);
  const link = document.createElement('a'); link.href = url; link.download = release.filename; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

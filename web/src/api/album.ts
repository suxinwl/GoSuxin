import { defHttp } from '@/utils/http';
import type { UploadFileParams } from '/#/axios';

const apiRoot =
  import.meta.env.VITE_APP_ENV === 'production'
    ? window.globalConfig.Root_url
    : window.globalConfig.Root_url_dev;

// Multipart uploads return the raw envelope, unlike defHttp.post/get.
// HTTP 200 alone does not mean the file was saved.
export class AlbumUploadRejected extends Error {
  constructor(message:string, public code:number) { super(message); }
}
async function checkedAlbumUpload(config: any, params: UploadFileParams) {
  const result = await defHttp.uploadFile(config, params);
  if (!result || typeof result !== 'object' || !('code' in result))
    throw new Error('上传响应无效，保存结果待确认');
  if (result.code !== 0)
    throw new AlbumUploadRejected(result.message || '服务器拒绝上传', result.code);
  return result;
}

export interface AlbumRecord {
  id: number;
  title: string;
  language: 'zh' | 'en';
  description?: string;
  category: string;
  category_id: number;
  sort_order: number;
  cover_url?: string;
  page_count: number;
  status: string;
  visibility: 'public' | 'private';
  failure_reason?: string;
  source_type: 'images' | 'pdf';
  processing_stage?: string;
}

export const getAlbums = (params: Record<string, unknown>) =>
  defHttp.get({ url: '/album/list', params }, { errorMessageMode: 'message' });

export const saveAlbum = (params: Record<string, unknown>) =>
  defHttp.post({ url: '/album/save', params }, { errorMessageMode: 'message' });

export const publishAlbum = (id: number, publish: boolean) =>
  defHttp.post(
    { url: '/album/publish', params: { id, publish } },
    { errorMessageMode: 'message' }
  );

export interface AlbumUploadProgress { loaded: number; total?: number }
export const uploadAlbumPage = (albumId: number, params: UploadFileParams, onUploadProgress?: (event: AlbumUploadProgress) => void) =>
  checkedAlbumUpload(
    { url: `${apiRoot}/album/page/add`, timeout: 1810000, onUploadProgress },
    { ...params, data: { albumId } }
  );

export const uploadAlbumPdf = (albumId: number, params: UploadFileParams) =>
  checkedAlbumUpload(
    { url: `${apiRoot}/album/pdf/upload`, timeout: 1810000 },
    { ...params, data: { albumId } }
  );

export const retryAlbumPdf = (albumId: number) =>
  defHttp.post(
    { url: '/album/pdf/retry', params: { albumId } },
    { errorMessageMode: 'message' }
  );

export const albumGet = (path: string, params: Record<string, unknown> = {}) =>
  defHttp.get(
    { url: `/album/${path}`, params },
    { errorMessageMode: 'message' }
  );
export const albumPost = (path: string, params: Record<string, unknown>) =>
  defHttp.post(
    { url: `/album/${path}`, params },
    { errorMessageMode: 'message' }
  );
export const replaceAlbumPage = (albumId: number, pageId: number, file: File) =>
  checkedAlbumUpload(
    { url: `${apiRoot}/album/page/replace`, timeout: 1810000 },
    { file, data: { albumId, pageId } }
  );

export const testAlbumStorage = (id: string) =>
  defHttp.post(
    { url: '/album/storage/test', params: { id }, timeout: 310000 },
    { errorMessageMode: 'message' }
  );
export const uploadCategoryCover = (id: number, language: string, file: File) =>
  checkedAlbumUpload(
    { url: `${apiRoot}/album/category/cover`, timeout: 1810000 },
    { file, data: { id, language } }
  );

export const uploadSiteLogo = (params: UploadFileParams) => checkedAlbumUpload({url: `${apiRoot}/album/site/logo`}, params);

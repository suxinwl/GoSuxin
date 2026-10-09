export const isEnglish =
  window.SuxinAlbumSite.language === 'en';
const strings = {
  brand: ['数字画册', 'Digital Catalogues'],
  home: ['首页', 'Home'],
  website: ['官网', 'Website'],
  unavailable: ['画册不可访问', 'Catalogue unavailable'],
  back: ['返回书架', 'Back to library'],
  exclusive: ['访问专属画册', 'Open your private catalogue'],
  privateHint: [
    '此画册仅限收到分享链接的访客阅读。',
    'This catalogue is available to visitors with a share link.',
  ],
  password: ['请输入分享密码', 'Enter the share password'],
  verifying: ['正在验证…', 'Verifying…'],
  open: ['打开画册', 'Open catalogue'],
  headline: [
    '随时随地，<br/>翻阅精彩。',
    'Discover our<br/>digital catalogues.',
  ],
  introduction: [
    '产品资料、公司介绍与耗材选型，随时随地翻阅。',
    'Product catalogues, company information and consumables, wherever you are.',
  ],
  browse: ['浏览产品画册', 'Browse catalogues'],
  choose: ['选择一个书架', 'Choose a collection'],
  chooseHint: [
    '按产品领域快速查找所需画册',
    'Find the information you need by product category',
  ],
  albums: ['本画册', 'catalogues'],
  books: ['本', 'catalogues'],
  catalogue: ['画册', 'Catalogue'],
  pages: ['页', 'pages'],
  loading: ['正在载入画册…', 'Loading catalogues…'],
  opening: ['正在打开画册…', 'Opening catalogue…'],
  contents: ['目录', 'Contents'],
  library: ['书架', 'Library'],
  openLibrary: ['打开书架', 'Open library'],
  close: ['关闭', 'Close'],
  categories: ['画册分类', 'Catalogue categories'],
  shelfCategories: ['书架分类', 'Collections'],
  empty: ['该书架暂无画册', 'No catalogues in this collection yet'],
  reading: ['正在阅读', 'Reading now'],
  imageError: [
    '图片不可访问或授权已失效，请重新打开画册。',
    'This image is unavailable or access has expired. Please reopen the catalogue.',
  ],
  fullscreenError: [
    '当前浏览器不支持全屏模式',
    'Fullscreen is not available in this browser.',
  ],
  requestError: [
    '请求失败，请稍后重试',
    'Unable to load this content. Please try again.',
  ],
  wrongPassword: ['分享密码错误', 'Incorrect password. Please try again.'],
  expired: ['分享链接已过期', 'This share link has expired.'],
  missingShare: [
    '分享链接不存在或已失效',
    'This share link is unavailable or has been disabled.',
  ],
  missingAlbum: [
    '公开画册不存在',
    'This catalogue is not available on this site.',
  ],
  offline: ['画册不可用', 'This catalogue is currently unavailable.'],
};
export const copy = Object.fromEntries(
  Object.entries(strings).map(([key, values]) => [
    key,
    values[isEnglish ? 1 : 0],
  ])
);
export function publicError(error) {
  if (!isEnglish) return error?.message || copy.requestError;
  const match = Object.values(strings).find(
    (values) => values[0] === error?.message
  );
  return match ? match[1] : copy.requestError;
}

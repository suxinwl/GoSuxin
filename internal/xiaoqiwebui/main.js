import { api, post, sourceLabel, normalizeSource, setMessage, dramaOperationID } from './ui-core.js';
import { createShell } from './shell.js';
import { createLibrary } from './library.js';
import { createDownloads } from './downloads.js';
import { createSettings } from './settings.js';
import { createFollowing } from './following.js';
import { createDetails } from './details.js';
import { initializeViewer } from './viewer.js';
import { createAccount } from './account.js';
import { createUsers } from './users.js';

const app = {api, post};
app.play = (id, title) => {
  const drama = app.library.get(id);
  const operationID = dramaOperationID(drama) || id;
  const routeID = drama?.workId || (String(drama?.id || '').startsWith('work:') ? drama.id : '') || operationID;
  const route = window.XiaoqiDialogs?.route?.();
  if (route?.kind !== 'play' || route.value !== routeID) window.XiaoqiDialogs?.navigatePlay?.(routeID);
  if (window.XiaoqiHistory.get(operationID)) window.dramaPlayer.openHistory(operationID, title);
  else window.dramaPlayer.open(id, title);
  app.details?.show?.(id);
  app.following.acknowledge(operationID);
  app.library.refreshDrama(id);
};
app.shell = createShell(app);
app.library = createLibrary(app);
app.downloads = createDownloads(app);
app.settings = createSettings(app);
app.following = createFollowing(app);
app.details = createDetails(app);
app.account = createAccount(app);
app.users = createUsers(app);

// The reference navigation uses `.navbar-logo`, while the legacy shell
// listens for `.brand`. Keep the shell's existing behaviour without making
// the page depend on one particular header implementation.
const brand = document.querySelector('.brand') || document.querySelector('.navbar-logo');
if (brand) brand.classList.add('brand');

// Reference-page modules (rankings and navigation helpers) are loaded
// independently of this module and use the shared app facade. Expose
// it before any asynchronous initialization starts so they can observe the
// library as soon as it becomes available.
window.app = app;
window.XiaoqiDialogs?.registerSnapshot?.({
  capture: () => app.library.snapshot?.(),
  restore: snapshot => app.library.restoreSnapshot?.(snapshot)
});

app.shell.init();
window.addEventListener('xiaoqiviewerchange', () => {
  document.getElementById('viewerNotice').textContent = '账号权限已变化，正在重新加载…';
  document.getElementById('viewerNotice').hidden = false;
  window.location.reload();
});

async function initialize() {
  try {
    app.viewer = await initializeViewer();
  } catch (error) {
    const notice = document.getElementById('viewerNotice');
    const retry = document.createElement('button');
    retry.type = 'button';
    retry.className = 'secondary';
    retry.textContent = '重试';
    retry.addEventListener('click', () => window.location.reload());
    notice.replaceChildren(document.createTextNode(error.message), retry);
    notice.hidden = false;
    return;
  }
if (app.account.init()) return;
app.users.init();
app.shell.access();
window.XiaoqiDialogs.listenRoute(route => {
  if (route.kind === 'category') app.library.setCategory?.(route.value);
  if (route.kind !== 'play') window.dramaPlayer?.disposeRoute?.();
});
window.XiaoqiHistory.init({
  api, post,
  sourceLabel: value => sourceLabel(normalizeSource(value) || value),
  getDrama: id => app.library.get(id),
  onChanged: () => {app.library.refreshFollowing(); app.following.render(); app.details.refresh(); app.downloads.render();},
  onError: message => setMessage(message, true),
  play: app.play
});
window.XiaoqiRankings.init({
  api, post,
  getSource: () => document.getElementById('sourceSelect').value,
  getDrama: id => app.library.get(id),
  sourceLabel,
  onLibraryChanged: () => app.library.refresh(),
  canDownload: () => !app.viewer?.onlineOnly,
  onDownloadsChanged: app.downloads.refresh,
  getDownloadQuality: app.downloads.quality,
  play: app.play
});
Promise.allSettled([app.settings.init(), app.library.init(), app.downloads.init(), app.following.init()]).then(async results => {
  const failure = results.find(result => result.status === 'rejected');
  if (failure) setMessage('部分功能未能初始化：' + (failure.reason?.message || '请刷新页面重试'), true);
  const route = window.XiaoqiDialogs.route();
  if (route.kind === 'category') app.library.setCategory?.(route.value);
  if (route.kind === 'play') {
    const drama = app.library.get(route.value) || await app.library.ensure?.(route.value);
    if (drama) app.play(route.value, drama.title || drama.name || '');
    else app.play(route.value, route.value === 'ch4k8uf4p' ? '\u5170\u9999\u5982\u6545' : '');
  }
  document.documentElement.dataset.ready = 'true';
});

}

initialize();

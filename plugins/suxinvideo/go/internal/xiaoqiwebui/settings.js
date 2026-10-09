import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createSettings(app) {
  let config = {};
function refreshConfigFields(){ $('downloadLocation').textContent=config.outputDir||'尚未设置';$('downloadLocation').title=config.outputDir||''; $('downloadDirectory').value=config.outputDirSetting||config.outputDir||'./小柒影视下载';$('downloadDirectoryHint').textContent=(config.restartRequired?'已保存新目录，重启后生效。':'')+'选择程序所在电脑的文件夹，或输入相对/绝对路径。重启后新合集使用新目录，原任务和文件保留。';$('downloadConcurrency').value=config.concurrency||2;$('requestConcurrency').value=config.requestConcurrency||2;$('requestInterval').value=config.requestIntervalMs||500;$('proxyMode').value=config.proxyMode||'auto';$('proxyURL').value=config.proxyMode==='subscription'?'':(config.proxyURL||'');$('proxySubscriptionURL').value=config.proxySubscriptionURL||'';$('proxyUsername').value='';$('proxyPassword').value='';$('proxyPassword').placeholder=config.proxyHasAuth?'已保存，留空保留认证':'';updateProxyFields();$('configText').textContent='当前输出：'+(config.outputDir||'')+' · 下载并发 '+(config.concurrency||2)+' · '+(config.network||'')+' · FFmpeg '+(config.ffmpeg||''); }

function updateProxyFields(){
  // Keep the endpoint and credentials editable so an administrator can enter
  // a custom proxy without first guessing which mode unlocks the fields.  If
  // any proxy value is entered while another mode is selected, saveSettings
  // promotes the selection to manual mode before validating it.
  for(const name of ['proxyURL','proxyUsername','proxyPassword','proxySubscriptionURL']){
    const field=$(name);
    field.disabled=false;
    field.setAttribute('aria-disabled','false');
  }
}

async function loadConfig(){try{config=await api('/api/ui/config');refreshConfigFields();}catch(error){$('configText').textContent='配置读取失败：'+error.message;}}

async function saveSettings(){
    const settings={outputDir:$('downloadDirectory').value.trim(),concurrency:number($('downloadConcurrency').value),requestConcurrency:number($('requestConcurrency').value),requestIntervalMs:number($('requestInterval').value)};
    try{
      let mode=$('proxyMode').value;const raw=$('proxyURL').value.trim();const username=$('proxyUsername').value;const password=$('proxyPassword').value;const subscription=$('proxySubscriptionURL').value.trim();
      if(mode==='subscription' || subscription){
        if(!subscription) throw new Error('请输入代理订阅地址');
        const subscriptionURL=new URL(subscription);if(!['http:','https:'].includes(subscriptionURL.protocol))throw new Error('订阅地址必须是 HTTP/HTTPS URL');
        settings.proxySubscriptionURL=subscription;
      } else {
        settings.proxySubscriptionURL='';
        if(mode!=='manual' && (raw||username||password)) mode='manual';
        if(mode!=='manual') settings.proxyURL=mode;
        else if(!(config.proxyHasAuth&&config.proxyMode==='manual'&&raw===config.proxyURL&&!username&&!password)){
        if(!raw) throw new Error('请输入代理地址');
        const endpoint=new URL(raw);if(!['http:','https:','socks5:','socks5h:'].includes(endpoint.protocol))throw new Error('仅支持 HTTP/HTTPS/SOCKS5 代理');if(username||password){endpoint.username=username;endpoint.password=password;}settings.proxyURL=endpoint.toString();
        }
      }
      $('saveSettingsBtn').disabled=true;config=await post('/api/ui/config',settings);refreshConfigFields();$('settingsStatus').textContent=config.restartRequired?'设置已保存；下载目录重启后生效，当前任务保持原路径':'设置已保存并应用';
    }catch(error){$('settingsStatus').textContent='保存失败：'+error.message;}finally{$('saveSettingsBtn').disabled=false;}
  }

async function checkNetwork(){ $('checkNetworkBtn').disabled=true;$('settingsStatus').textContent='正在检测已保存的网络配置和红果入口，最多约一分钟…';try{const result=await post('/api/ui/network/check',{});$('settingsStatus').textContent=(result.data||[]).map(item=>item.name+'：'+(item.error||[item.status?'HTTP '+item.status:'',item.detail||''].filter(Boolean).join(' · '))).join('；');}catch(error){$('settingsStatus').textContent='检测失败：'+error.message;}finally{$('checkNetworkBtn').disabled=false;} }

async function chooseDownloadDirectory(){const button=$('browseDirectoryBtn');if(button.disabled)return;button.disabled=true;$('settingsStatus').textContent='请在系统窗口中选择下载文件夹…';try{const result=await post('/api/ui/directory/pick',{initialPath:$('downloadDirectory').value});if(result.canceled){$('settingsStatus').textContent='已取消选择，原目录未修改';return;}$('downloadDirectory').value=result.selectionPath||result.path;$('settingsStatus').textContent='已选择文件夹，请点击“保存并应用”';}catch(error){$('settingsStatus').textContent='选择文件夹失败：'+error.message;}finally{button.disabled=false;}}

async function importLegacyRecords() {
  const control = $('legacyImportBtn');
  control.disabled = true;
  $('viewerImportStatus').textContent = '正在接回旧版记录…';
  try {
    await post('/api/ui/viewer/legacy', {code: $('legacyImportCode').value.trim()});
    $('legacyImportCode').value = '';
    $('legacyImportSection').hidden = true;
    app.viewer.legacyAvailable = false;
    await Promise.all([window.XiaoqiHistory.refresh(), app.following.refresh()]);
    $('viewerImportStatus').textContent = app.viewer?.account ? '旧版记录已接入当前账号，原始文件仍保留。' : '旧版记录已接回当前浏览器，原始文件仍保留。';
  } catch (error) {
    $('viewerImportStatus').textContent = error.message;
  } finally {
    control.disabled = false;
  }
}

let sourceConfig = { sources: [], fullCategories: false };

async function loadSources() {
  try {
    const data = await api('/api/ui/admin/sources');
    sourceConfig = data;
    if ($('adminAdFilter')) {
      $('adminAdFilter').checked = data.adFilter !== false;
    }
    if ($('adminFullCategories')) {
      $('adminFullCategories').checked = Boolean(data.fullCategories);
    }
    if ($('adminSourcePriority')) {
      $('adminSourcePriority').value = (data.priority || []).join(',');
    }
    renderSourceInputs(data.sources || []);
  } catch (error) {
    if ($('sourceSettingsStatus')) {
      $('sourceSettingsStatus').textContent = '片源配置读取失败：' + error.message;
    }
  }
}

function renderSourceInputs(sources) {
  const container = $('adminSourceList');
  if (!container) return;
  empty(container);
  for (const src of sources) {
    const item = element('div', 'admin-source-item');
    const label = element('label', '', src.name || src.id);
    label.htmlFor = 'source_url_' + src.id;
    const input = element('input');
    input.id = 'source_url_' + src.id;
    input.dataset.sourceId = src.id;
    input.type = 'url';
    input.value = src.baseURL || '';
    input.placeholder = src.defaultURL || '默认接口地址';
    item.append(label, input);
    container.appendChild(item);
  }
}

async function saveSources() {
  const btn = $('saveSourcesBtn');
  if (!btn) return;
  btn.disabled = true;
  $('sourceSettingsStatus').textContent = '正在保存片源设置…';
  try {
    const sources = {};
    for (const input of $('adminSourceList').querySelectorAll('input[data-source-id]')) {
      const id = input.dataset.sourceId;
      sources[id] = input.value.trim();
    }
    const fullCategories = $('adminFullCategories')?.checked;
    const adFilter = $('adminAdFilter') ? $('adminAdFilter').checked : true;
    const priority = ($('adminSourcePriority')?.value || '').split(',').map(value => value.trim()).filter(Boolean);
    await post('/api/ui/admin/sources', {
      fullCategories,
      adFilter,
      sources,
      priority
    });
    $('sourceSettingsStatus').textContent = '片源设置已保存并生效，下次同步将应用';
    await loadSources();
  } catch (error) {
    $('sourceSettingsStatus').textContent = '保存失败：' + error.message;
  } finally {
    btn.disabled = false;
  }
}

function refreshAdminState() {
  const admin = Boolean(app.viewer?.account?.admin && !app.viewer.account.requirePasswordChange);
  $('serverSettings').hidden = !admin;
  $('serverSettingsNotice').hidden = admin;
  $('downloadLocationBtn').hidden = !admin;
  if (!admin) $('downloadLocation').textContent = '服务端下载目录';
  if (admin) {
    loadConfig();
    loadSources();
  }
}

function init() {
  refreshAdminState();
  $('legacyImportSection').hidden = !app.viewer?.legacyAvailable;
  $('viewerImportStatus').textContent = app.viewer?.legacyError || '';
  $('legacyImportBtn').addEventListener('click', importLegacyRecords);
  $('browseDirectoryBtn').addEventListener('click', chooseDownloadDirectory);
  $('proxyMode').addEventListener('change', () => {if($('proxyMode').value!=='subscription')$('proxySubscriptionURL').value='';updateProxyFields();});
  $('saveSettingsBtn').addEventListener('click', saveSettings);
  $('checkNetworkBtn').addEventListener('click', checkNetwork);
  $('saveSourcesBtn')?.addEventListener('click', saveSources);
  $('openSettingsBtn').addEventListener('click', () => {
    refreshAdminState();
    window.XiaoqiDialogs.open('settingsPanel');
  });
  $('closeSettingsBtn').addEventListener('click', () => $('settingsPanel').close());
  $('themeSelect').value = window.XiaoqiTheme.get();
  $('themeSelect').addEventListener('change', () => window.XiaoqiTheme.set($('themeSelect').value));
  document.addEventListener('themechange', event => {$('themeSelect').value = event.detail;});
  const admin = Boolean(app.viewer?.account?.admin && !app.viewer.account.requirePasswordChange);
  return admin ? Promise.all([loadConfig(), loadSources()]) : Promise.resolve();
}
return {init, config: () => config, refreshAdminState};

}

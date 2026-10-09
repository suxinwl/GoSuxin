// 频道别名管理 API（issue #56）—— 可视化维护 data/channel-aliases.json
//
// 格式：{ "规范名": ["别名1", "别名2", ...] }
// 用途：normalizeKey 兜不住的写法（如「央视5套」之外的语义别名），在这里定义一条规则即可批量统一。
// channelNormalize.getCanonicalMap() 已按文件 mtime 读取该文件，保存即在下次生成播放列表时生效（无需重启）。

import { existsSync, readFileSync } from 'node:fs'
import { writeJsonFileSync } from './fileUtil.js'
import { dataPath } from './paths.js'

const ALIASES_PATH = dataPath('channel-aliases.json')

function load() {
  if (!existsSync(ALIASES_PATH)) return {}
  try {
    const o = JSON.parse(readFileSync(ALIASES_PATH, 'utf-8'))
    return (o && typeof o === 'object' && !Array.isArray(o)) ? o : {}
  } catch {
    return {}
  }
}

export function getAliasesAPI() {
  try {
    return { success: true, data: load() }
  } catch (e) {
    return { success: false, message: e.message }
  }
}

// 新增 / 更新一条规则：canonical（统一后的规范名）+ aliases（别名，数组或逗号/换行分隔的字符串）
export function setAliasRuleAPI(canonical, aliases) {
  try {
    const name = typeof canonical === 'string' ? canonical.trim() : ''
    if (!name) return { success: false, message: '请填写规范名（统一后的频道名）' }
    let list = Array.isArray(aliases) ? aliases : String(aliases || '').split(/[,，\n]/)
    list = [...new Set(list.map(s => String(s).trim()).filter(Boolean))]
    if (list.length === 0) return { success: false, message: '请至少填写一个别名' }
    const obj = load()
    obj[name] = list
    writeJsonFileSync(ALIASES_PATH, obj)
    return { success: true, data: obj }
  } catch (e) {
    return { success: false, message: e.message }
  }
}

// 删除一条规则
export function removeAliasRuleAPI(canonical) {
  try {
    const obj = load()
    if (!(canonical in obj)) return { success: false, message: '规则不存在' }
    delete obj[canonical]
    writeJsonFileSync(ALIASES_PATH, obj)
    return { success: true, data: obj }
  } catch (e) {
    return { success: false, message: e.message }
  }
}

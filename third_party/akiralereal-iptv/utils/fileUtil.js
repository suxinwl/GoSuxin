import fs from "node:fs"
function createFile(filePath) {
  if (!fs.existsSync(filePath)) {
    writeFile(filePath, "")
  }
}

function writeFile(filePath, content) {
  fs.writeFile(filePath, content, error => {
    if (error) {
      throw new Error(`${filePath}:写入${content}失败`)
    }
  })
}

function appendFile(filePath, content) {
  fs.appendFile(filePath, content, error => {
    if (error) {
      throw new Error(`${filePath}:追加${content}失败`)
    }
  })
}

function appendFileSync(filePath, content) {
  // 同步 API 不接受回调，旧写法传入回调会被当成 options 参数
  fs.appendFileSync(filePath, content)
}

function writeFileSync(filePath, content) {
  fs.writeFileSync(filePath, content)
}

function readFileSync(filePath) {
  return fs.readFileSync(filePath)
}

function renameFileSync(oldFilePath, newFilePath) {
  // 同步 API 不接受回调
  fs.renameSync(oldFilePath, newFilePath)
}
function copyFileSync(filePath, newFilePath, mode) {
  // 同步 API 不接受回调
  fs.copyFileSync(filePath, newFilePath, mode)
}

/**
 * 原子写入 JSON 文件：先写临时文件再 rename 覆盖，
 * 避免写入中途崩溃 / 并发写入导致 JSON 文件损坏。
 * @param {string} filePath - 目标文件路径
 * @param {object|string} data - 对象（自动 JSON 序列化）或已序列化的字符串
 */
function writeJsonFileSync(filePath, data) {
  const content = typeof data === 'string' ? data : JSON.stringify(data, null, 2)
  const tmpPath = `${filePath}.tmp`
  fs.writeFileSync(tmpPath, content, 'utf-8')
  fs.renameSync(tmpPath, filePath)
}

export { createFile, writeFile, appendFile, appendFileSync, writeFileSync, readFileSync, renameFileSync, copyFileSync, writeJsonFileSync }

# 扩展工具与业务复用

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

utility/auth 负责认证，utility/extend/uploads 负责统一上传，utility/extend/pan123 对接云盘，utility/extend/uploadconfig 处理存储配置。utility/gf 是兼容保留的业务辅助包名，包含响应、配置和 URL 等工具；新模块可以用 import 别名 suxin 调用它，不需要改变已有数据库或接口结构。

新增工具应明确输入输出、可取消的 context 和错误约定。公共工具不要直接读取当前用户全局变量；由调用方传入身份或上下文。网络调用设置超时，临时文件使用 defer 清理。对配置写入保持结构化及原子替换，不用字符串替换修改包含密钥的 YAML。

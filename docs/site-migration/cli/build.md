# Suxin CLI：编译与交叉编译

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```bash
suxin build -h
go build -o bin/gosuxin .
```
CLI 构建读取 hack/config.yaml 中 suxincli.build，包括 name、path、arch、system 和版本等。只构建本机时可直接使用 go build；跨平台前确认依赖是否需要 CGO 和对应交叉编译器。SQLite 和 Oracle 等驱动需结合使用的底层库核验目标平台能力。

程序、manifest/config、resource 静态文件和安装资源各有用途；没有嵌入的运行资源需要一并部署。发布前检查压缩包不要包含 runtime、数据库备份、本机配置或 .git。框架维护者发布每个子模块时使用目录前缀标签。

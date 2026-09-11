# Suxin CLI：安装与版本

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

源码开发可在仓库根目录执行：
```bash
go build -o bin/suxin ./framework/cmd/suxin
```
发布完成后使用固定版本安装：
```bash
go install github.com/suxinwl/GoSuxin/framework/cmd/suxin@v1.0.0
suxin version
suxin -h
```
把 go env GOPATH 对应的 bin 加入 PATH。Windows Release 二进制可重命名为 suxin.exe 放入自己的工具目录。version 应输出 v1.0.0；若仍是其他程序，检查终端 PATH 和 where suxin。上面的远程安装命令需以发布验证报告中的结果为准。

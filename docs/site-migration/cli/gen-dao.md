# Suxin CLI：DAO 与数据模型生成

适用：Suxin Framework V1.0.0 / Suxin CLI V1.0.0。

```yaml
suxincli:
  gen:
    dao:
    - link: "mysql:demo:CHANGE_ME@tcp(127.0.0.1:3306)/demo"
      tables: "sx_note"
      removePrefix: "sx_"
      path: "internal"
```
上述配置放在开发机 hack/config.yaml，凭证不要提交。执行 suxin gen dao 生成 dao、model/do 和 model/entity。tables 用于限制生成范围，removePrefix 控制 Go 类型名，而非修改数据库表名。不要在生产库尝试教程建表。

DAO 封装表名及访问入口，DO 用于条件或写入，Entity 表示读取记录。生成模型不创建数据库表。修改字段后重新生成并检查业务代码编译；不要直接在可再生成文件里写业务规则。

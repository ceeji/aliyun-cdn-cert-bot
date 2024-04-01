# aliyun-cdn-cert-bot
Update your Aliyun's CDN certificate automatically. 自动更新你的阿里云 CDN 证书。

再也不需要安装繁重的 Python。使用轻巧的 Golang 更新你的阿里云 CDN 证书，可以结合 acme.sh、let's encrypt 免费使用证书。

# Documents / 文档

首先在项目所在文件夹中安装依赖：

```
go get "github.com/denverdino/aliyungo/cdn"
```

然后编译 `main.go` 即可使用。

## 环境变量

运行之前请设置下列环境变量：

`ACCESS_KEY_ID`、`ACCESS_KEY_SECRET` 为阿里云有权限的 RAM 子账号信息；
`ALI_DOMAIN` 为阿里云 CDN 域名（非源站域名）；
`ALI_CERT_PATH` 为 CDN 证书文件名（注意要用 fullchain 的证书，否则可能有些客户端会报错）；
`ALI_KEY_PATH` 为 CDN 证书密钥文件名。

输出错误为 nil 即为成功。

建议结合 `crontab` 设置定时任务，每天执行一次。

对于 `acme.sh` 用户，你可以直接设置相关路径到 `~/.acme.sh/证书名称/文件` 这样的路径。

# Showcase / 案例

- [ceeji.net](https://ceeji.net "笃志者")
- [zhihuitonghua.baiyan.tech](zhihuitonghua.baiyan.tech "智绘童话")

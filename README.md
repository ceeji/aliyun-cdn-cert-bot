# aliyun-cdn-cert-bot
Update your Aliyun's CDN certificate automatically. 自动更新你的阿里云 CDN 证书。

再也不需要安装繁重的 Python。使用轻巧的 Golang 更新你的阿里云 CDN 证书，可以结合 acme.sh、let's encrypt 免费使用证书。

## 安全部署与验证

默认读取可执行文件旁的 `config.yml`。配置包含凭据时使用 `0600`，证书私钥也应仅允许部署用户读取。

```sh
./aliyun-cdn-cert-bot --check --no-notify       # 检查全部配置、域名、有效期、证书与私钥匹配；无云请求
./aliyun-cdn-cert-bot --verify --no-notify      # 只读比较 CDN/OSS 云端证书指纹；不一致时返回非零
./aliyun-cdn-cert-bot --no-notify               # 云端一致则跳过，不一致则部署并回读验证
./aliyun-cdn-cert-bot --force --no-notify       # 明确需要实测写权限时重新部署现有证书；不会签发证书
```

可用 `--config /path/config.yml` 指定配置。所有目标先通过本地预检，才进行任何写入；读云端状态失败时不修改该目标。CDN 使用 `DescribeDomainCertificateInfo`，OSS 使用 `ListCname` 比较 SHA-1/SHA-256 指纹。APISIX 和 Kubernetes Secret 部署方式保持兼容，但 `--verify` 仅支持 CDN/OSS。

定时任务和 acme reload 钩子应共用一个 `flock` 包装入口，避免并发换证。日常巡检建议使用 `--no-notify`；原 webhook 仅按所需的自动续期钩子保留。程序不签发证书，也不管理 ACME 账号或 DNS 记录。

# Documents / 文档

[文档地址](https://ceeji.net/blog/update-aliyun-cdn-cert)

# Showcase / 案例

- [笃志者](https://ceeji.net "笃志者")
- [百言 AI](https://ai.baiyan.tech "百言AI")
- [百言科技](https://www.baiyan.tech "百言科技")

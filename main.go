package main

import (
	"fmt"
	"os"
	"time"

	cas20200407 "github.com/alibabacloud-go/cas-20200407/v2/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/denverdino/aliyungo/cdn"
)

func main() {
	// 调试专用
	// os.Setenv("ALI_ACCESS_KEY_ID", "")
	// os.Setenv("ALI_ACCESS_KEY_SECRET", "")
	// os.Setenv("ALI_DOMAIN", "")
	// os.Setenv("ALI_OSS_BUCKET", "")
	// os.Setenv("ALI_CERT_PATH", "")
	// os.Setenv("ALI_KEY_PATH", "")
	// os.Setenv("ALI_OSS_ENDPOINT", "")
	// os.Setenv("ALI_OSS_REGION", "")

	var ACCESS_KEY_ID = os.Getenv("ALI_ACCESS_KEY_ID")
	var ACCESS_KEY_SECRET = os.Getenv("ALI_ACCESS_KEY_SECRET")
	var domain = os.Getenv("ALI_DOMAIN")
	var certPath = os.Getenv("ALI_CERT_PATH")
	var keyPath = os.Getenv("ALI_KEY_PATH")
	var ossBucket = os.Getenv("ALI_OSS_BUCKET")
	// acme.sh 前两行

	var cert []byte
	var key []byte
	var err error
	if cert, err = os.ReadFile(certPath); err != nil {
		panic(err)
	}
	if key, err = os.ReadFile(keyPath); err != nil {
		panic(err)
	}

	// 生成一个不重复的证书名称
	var certName = "cert" + time.Now().Format("20060102150405")

	// 记录日志
	fmt.Println("time: ", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("update cert for domain: ", domain)
	fmt.Println("certName: ", certName)
	if ossBucket != "" {
		fmt.Println("ossBucket: ", ossBucket)
	}

	// 对于 CDN
	if ossBucket == "" {
		client := cdn.NewClient(ACCESS_KEY_ID, ACCESS_KEY_SECRET)
		res, err := client.SetDomainServerCertificate(cdn.CertificateRequest{
			DomainName:              domain,
			CertName:                certName,
			ServerCertificateStatus: "on",
			ServerCertificate:       string(cert),
			PrivateKey:              string(key),
		})

		fmt.Printf("res: %v, err: %v\n", res, err)
		return
	}

	// 对于 OSS
	if ossBucket != "" {
		os.Setenv("OSS_ACCESS_KEY_ID", ACCESS_KEY_ID)
		os.Setenv("OSS_ACCESS_KEY_SECRET", ACCESS_KEY_SECRET)
		region := os.Getenv("ALI_OSS_REGION")
		if region == "" {
			fmt.Println("Error: OSS_REGION is required")
			os.Exit(-1)
		}
		endpoint := os.Getenv("ALI_OSS_ENDPOINT")

		// 证书管理服务客户端
		casClient, err := cas20200407.NewClient(&openapi.Config{
			AccessKeyId:     tea.String(ACCESS_KEY_ID),
			AccessKeySecret: tea.String(ACCESS_KEY_SECRET),

			// endpoint参考：https://api.aliyun.com/product/cas
			Endpoint: tea.String("cas.aliyuncs.com"),
		})
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(-1)
		}
		req := new(cas20200407.UploadUserCertificateRequest)
		// 证书名称
		req.Name = tea.String(certName)
		// 证书私钥
		req.Key = tea.String(string(key))
		// 证书内容
		req.Cert = tea.String(string(cert))
		// 上传证书到证书管理服务
		resp, err := casClient.UploadUserCertificate(req)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(-1)
		}

		if *resp.StatusCode != 200 {
			fmt.Println("Error:", resp.Body)
			os.Exit(-1)
		}

		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(-1)
		}

		certId := resp.Body.CertId

		// 创建OSSClient实例。
		// yourEndpoint填写Bucket对应的Endpoint，以华东1（杭州）为例，填写为https://oss-cn-hangzhou.aliyuncs.com。其它Region请按实际情况填写。
		client, err := oss.New(endpoint, ACCESS_KEY_ID, ACCESS_KEY_SECRET)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(-1)
		}

		var putCnameConfig oss.PutBucketCname
		var CertificateConfig oss.CertificateConfiguration
		// 填写自定义域名。
		putCnameConfig.Cname = domain
		// 填写证书ID。
		CertificateConfig.CertId = fmt.Sprintf("%d-%s", *certId, os.Getenv("OSS_REGION"))
		// CertificateConfig.Certificate = string(cert)
		// CertificateConfig.PrivateKey = string(key)
		CertificateConfig.Force = true
		putCnameConfig.CertificateConfiguration = &CertificateConfig
		err = client.PutBucketCnameWithCertificate(ossBucket, putCnameConfig)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(-1)
		}

		fmt.Printf("Bind Certificate Success!")
	}
}

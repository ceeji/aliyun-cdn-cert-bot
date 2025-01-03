package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	cas20200407 "github.com/alibabacloud-go/cas-20200407/v2/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/denverdino/aliyungo/cdn"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Projects []Project `yaml:"projects"`
}

type Project struct {
	Name            string `yaml:"name"`
	Mode            string `yaml:"mode"`
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	Domain          string `yaml:"domain"`
	CertPath        string `yaml:"cert_path"`
	KeyPath         string `yaml:"key_path"`
	OssBucket       string `yaml:"oss_bucket,omitempty"`
	OssEndpoint     string `yaml:"oss_endpoint,omitempty"`
	OssRegion       string `yaml:"oss_region,omitempty"`
	ApisixAdminURL  string `yaml:"apisix_admin_url,omitempty"` // 新增字段，用于 APISIX 模式
	ApisixAdminKey  string `yaml:"apisix_admin_key,omitempty"` // 新增字段，用于 APISIX 模式
}

func main() {
	// 获取程序可执行文件所在目录
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	configFile := filepath.Join(dir, "config.yml")
	configData, err := os.ReadFile(configFile)
	if err != nil {
		fmt.Printf("[ERROR] Failed to read config file: %v\n", err)
		os.Exit(1)
	}

	var config Config
	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		fmt.Printf("[ERROR] Failed to parse config file: %v\n", err)
		os.Exit(1)
	}

	totalProjects := len(config.Projects)
	successCount := 0
	failures := []string{}

	fmt.Printf("========== Starting Certificate Update ==========\n")
	fmt.Printf("Total Projects: %d\n", totalProjects)

	for _, project := range config.Projects {
		fmt.Printf("\n[INFO] Processing project: %s (Domain: %s, Mode: %s)\n", project.Name, project.Domain, project.Mode)
		err := updateCertificate(project)
		if err != nil {
			fmt.Printf("[ERROR] Failed to update certificate for project '%s': %v\n", project.Name, err)
			failures = append(failures, project.Name)
		} else {
			fmt.Printf("[SUCCESS] Certificate updated successfully for project '%s'\n", project.Name)
			successCount++
		}
	}

	failCount := totalProjects - successCount
	fmt.Printf("\n========== Summary ==========\n")
	fmt.Printf("Total Projects: %d\n", totalProjects)
	fmt.Printf("Successful: %d\n", successCount)
	fmt.Printf("Failed: %d\n", failCount)

	if failCount > 0 {
		fmt.Printf("\nFailed Projects:\n")
		for _, failure := range failures {
			fmt.Printf("- %s\n", failure)
		}
	}

	fmt.Printf("\n========== Process Completed ==========\n")
}

// extractDomainsFromCert 从证书中提取域名
func extractDomainsFromCert(certPEM string) ([]string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %v", err)
	}

	// Collect domains from SAN (Subject Alternative Names)
	var domains []string
	domains = append(domains, cert.DNSNames...)

	return domains, nil
}

func updateCertificate(project Project) error {
	var cert []byte
	var key []byte
	var err error

	if cert, err = os.ReadFile(project.CertPath); err != nil {
		return fmt.Errorf("failed to read certificate file: %v", err)
	}
	if key, err = os.ReadFile(project.KeyPath); err != nil {
		return fmt.Errorf("failed to read key file: %v", err)
	}

	// Generate a unique certificate name
	certName := "cert" + time.Now().Format("20060102150405.000")

	fmt.Printf("[INFO] Generated certificate name: %s\n", certName)

	switch project.Mode {
	case "alicdn":
		client := cdn.NewClient(project.AccessKeyID, project.AccessKeySecret)
		res, err := client.SetDomainServerCertificate(cdn.CertificateRequest{
			DomainName:              project.Domain,
			CertName:                certName,
			ServerCertificateStatus: "on",
			ServerCertificate:       string(cert),
			PrivateKey:              string(key),
		})
		if err != nil {
			return fmt.Errorf("failed to update certificate via Alicdn: %v", err)
		}
		fmt.Printf("[INFO] Alicdn response: %v\n", res)
	case "alioss":
		os.Setenv("OSS_ACCESS_KEY_ID", project.AccessKeyID)
		os.Setenv("OSS_ACCESS_KEY_SECRET", project.AccessKeySecret)
		if project.OssRegion == "" {
			return fmt.Errorf("OSS_REGION is required")
		}

		casClient, err := cas20200407.NewClient(&openapi.Config{
			AccessKeyId:     tea.String(project.AccessKeyID),
			AccessKeySecret: tea.String(project.AccessKeySecret),
			Endpoint:        tea.String("cas.aliyuncs.com"),
		})
		if err != nil {
			return fmt.Errorf("failed to create CAS client: %v", err)
		}

		req := new(cas20200407.UploadUserCertificateRequest)
		req.Name = tea.String(certName)
		req.Key = tea.String(string(key))
		req.Cert = tea.String(string(cert))
		resp, err := casClient.UploadUserCertificate(req)
		if err != nil {
			return fmt.Errorf("failed to upload certificate to CAS: %v", err)
		}
		if *resp.StatusCode != 200 {
			return fmt.Errorf("CAS response error: %v", resp.Body)
		}

		certId := resp.Body.CertId
		client, err := oss.New(project.OssEndpoint, project.AccessKeyID, project.AccessKeySecret)
		if err != nil {
			return fmt.Errorf("failed to create OSS client: %v", err)
		}

		putCnameConfig := oss.PutBucketCname{
			Cname: project.Domain,
			CertificateConfiguration: &oss.CertificateConfiguration{
				CertId: fmt.Sprintf("%d-%s", *certId, project.OssRegion),
				Force:  true,
			},
		}
		err = client.PutBucketCnameWithCertificate(project.OssBucket, putCnameConfig)
		if err != nil {
			return fmt.Errorf("failed to bind certificate to OSS: %v", err)
		}

		fmt.Printf("[INFO] Certificate bound successfully to OSS\n")
	case "apisix":
		if project.ApisixAdminURL == "" || project.ApisixAdminKey == "" {
			return fmt.Errorf("APISIX admin URL and key are required for APISIX mode")
		}

		snis, err := extractDomainsFromCert(string(cert))
		if err != nil {
			return fmt.Errorf("failed to extract domains from certificate: %v", err)
		}

		// Prepare the payload for APISIX Admin API
		payload := map[string]interface{}{
			"id":   project.Name, // Use name as the certificate ID
			"cert": string(cert),
			"key":  string(key),
			"snis": snis,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %v", err)
		}

		// Send the request to APISIX Admin API
		req, err := http.NewRequest("PUT", fmt.Sprintf("%s/apisix/admin/ssl/%s", project.ApisixAdminURL, project.Domain), bytes.NewBuffer(payloadBytes))
		if err != nil {
			return fmt.Errorf("failed to create HTTP request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-KEY", project.ApisixAdminKey)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request to APISIX Admin API: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("APISIX Admin API responded with status %d: %s", resp.StatusCode, string(body))
		}

		fmt.Printf("[INFO] Certificate updated successfully in APISIX\n")
	default:
		return fmt.Errorf("unsupported mode: %s", project.Mode)
	}

	return nil
}

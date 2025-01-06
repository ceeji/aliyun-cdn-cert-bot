package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Projects             []Project `yaml:"projects"`
	QiyewechatWebhookUrl string    `yaml:"qiyewechat_webhook_url"`
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
	ApisixAdminURL  string `yaml:"apisix_admin_url,omitempty"` //  APISIX 模式
	ApisixAdminKey  string `yaml:"apisix_admin_key,omitempty"` //  APISIX 模式
	K8sNamespace    string `yaml:"k8s_namespace,omitempty"`    // Kubernetes namespace
	K8sSecretName   string `yaml:"k8s_secret_name,omitempty"`  // Kubernetes secret name
}

var allOutput string

func writeOutput(format string, values ...any) {
	fmt.Printf(format, values...)
	allOutput += fmt.Sprintf(format, values...)
}

func main() {
	// 获取程序可执行文件所在目录
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	configFile := filepath.Join(dir, "config.yml")
	configData, err := os.ReadFile(configFile)
	if err != nil {
		writeOutput("[ERROR] Failed to read config file: %v\n", err)
		os.Exit(1)
	}

	var config Config
	err = yaml.Unmarshal(configData, &config)
	if err != nil {
		writeOutput("[ERROR] Failed to parse config file: %v\n", err)
		os.Exit(1)
	}

	totalProjects := len(config.Projects)
	successCount := 0
	failures := []string{}

	writeOutput("========== Starting Certificate Update ==========\n")
	writeOutput("Total Projects: %d\n", totalProjects)

	for _, project := range config.Projects {
		writeOutput("\n[INFO] Processing project: %s (Domain: %s, Mode: %s)\n", project.Name, project.Domain, project.Mode)
		err := updateCertificate(project)
		if err != nil {
			writeOutput("[ERROR] Failed to update certificate for project '%s': %v\n", project.Name, err)
			failures = append(failures, project.Name)
		} else {
			writeOutput("[SUCCESS] Certificate updated successfully for project '%s'\n", project.Name)
			successCount++
		}
	}

	failCount := totalProjects - successCount
	writeOutput("\n========== Summary ==========\n")
	writeOutput("Total Projects: %d\n", totalProjects)
	writeOutput("Successful: %d\n", successCount)
	writeOutput("Failed: %d\n", failCount)

	if failCount > 0 {
		writeOutput("\nFailed Projects:\n")
		for _, failure := range failures {
			writeOutput("- %s\n", failure)
		}
	}

	writeOutput("\n========== Process Completed ==========\n")

	if config.QiyewechatWebhookUrl != "" {
		sendAlert(config.QiyewechatWebhookUrl, allOutput)
	}

	if failCount > 0 {
		os.Exit(1)
	}
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

	writeOutput("[INFO] Generated certificate name: %s\n", certName)

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
		writeOutput("[INFO] Alicdn response: %v\n", res)
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

		writeOutput("[INFO] Certificate bound successfully to OSS\n")
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
			"cert": string(cert),
			"key":  string(key),
			"snis": snis,
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %v", err)
		}

		// Send the request to APISIX Admin API
		encodedProjectName := url.QueryEscape(project.Name)
		req, err := http.NewRequest("PUT", fmt.Sprintf("%s/apisix/admin/ssls/%s", project.ApisixAdminURL, encodedProjectName), bytes.NewBuffer(payloadBytes))
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

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("APISIX Admin API responded with status %d: %s", resp.StatusCode, string(body))
		}

		writeOutput("[INFO] Certificate updated successfully in APISIX\n")
	case "k8s-secret":
		clientset, err := getClientset()
		if err != nil {
			return fmt.Errorf("filed to get kuernates clientset: %+v", err)
		}
		err = updateK8sSecretCert(clientset, project, cert, key)
		if err != nil {
			return err
		}
		writeOutput("[INFO] kubernates' secret updated successfully\n")
	default:
		return fmt.Errorf("unsupported mode: %s", project.Mode)
	}

	return nil
}

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
}

func main() {
	configFile := "config.yml"
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
		return fmt.Errorf("APISIX mode is not implemented yet")
	default:
		return fmt.Errorf("unsupported mode: %s", project.Mode)
	}

	return nil
}

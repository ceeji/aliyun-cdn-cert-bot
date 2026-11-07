package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/denverdino/aliyungo/cdn"
)

type material struct {
	cert, key []byte
	leaf      *x509.Certificate
}

func loadMaterial(p Project) (material, error) {
	m := material{}
	var err error
	if m.cert, err = os.ReadFile(p.CertPath); err != nil {
		return m, err
	}
	if m.key, err = os.ReadFile(p.KeyPath); err != nil {
		return m, err
	}
	pair, err := tls.X509KeyPair(m.cert, m.key)
	if err != nil {
		return m, fmt.Errorf("certificate/private key mismatch or invalid PEM")
	}
	m.leaf, err = x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return m, err
	}
	now := time.Now()
	if now.Before(m.leaf.NotBefore) || !now.Before(m.leaf.NotAfter) {
		return m, fmt.Errorf("certificate is outside its validity period")
	}
	if p.Domain != "" {
		if err = m.leaf.VerifyHostname(p.Domain); err != nil {
			return m, err
		}
	}
	if m.leaf.NotAfter.Sub(now) < 30*24*time.Hour {
		writeOutput("[WARNING] %s expires %s; confirm renewal source\n", p.Name, m.leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	switch p.Mode {
	case "alicdn", "alioss":
		if p.Domain == "" || p.AccessKeyID == "" || p.AccessKeySecret == "" {
			return m, fmt.Errorf("domain and credentials are required")
		}
		if p.Mode == "alioss" && (p.OssRegion == "" || p.OssBucket == "" || p.OssEndpoint == "") {
			return m, fmt.Errorf("OSS region, bucket and endpoint are required")
		}
	case "apisix":
		if p.ApisixAdminURL == "" || p.ApisixAdminKey == "" {
			return m, fmt.Errorf("APISIX URL and key are required")
		}
	case "k8s-secret":
		if p.K8sNamespace == "" || p.K8sSecretName == "" {
			return m, fmt.Errorf("Kubernetes namespace and secret are required")
		}
	default:
		return m, fmt.Errorf("unsupported mode %s", p.Mode)
	}
	return m, nil
}

// Validate every target before the first write. The exact validated bytes are
// deployed, even if an ACME process replaces the source files during this run.
func processProjects(projects []Project, check, verify, force bool,
	read func(Project, *x509.Certificate) (bool, error),
	update func(Project, []byte, []byte) error) error {
	if len(projects) == 0 {
		return fmt.Errorf("no projects configured")
	}
	materials := make([]material, len(projects))
	seen := map[string]bool{}
	for i, p := range projects {
		id := p.Mode + "/" + p.OssBucket + "/" + p.Domain
		if p.Mode == "apisix" {
			id += "/" + p.ApisixAdminURL + "/" + p.Name
		}
		if p.Mode == "k8s-secret" {
			id += "/" + p.K8sNamespace + "/" + p.K8sSecretName
		}
		if seen[id] {
			err := fmt.Errorf("duplicate target %s", p.Name)
			writeOutput("[ERROR] %v\n", err)
			return err
		}
		seen[id] = true
		m, err := loadMaterial(p)
		if err != nil {
			writeOutput("[ERROR] preflight %s: %v; no updates performed\n", p.Name, err)
			return err
		}
		materials[i] = m
		writeOutput("[VALID] %s (%s) expires %s\n", p.Name, p.Domain, m.leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if check {
		return nil
	}
	failures := 0
	for i, p := range projects {
		m := materials[i]
		if p.Mode == "alicdn" || p.Mode == "alioss" {
			matched, err := read(p, m.leaf)
			if err != nil {
				writeOutput("[ERROR] read %s: %v; target not changed\n", p.Name, err)
				failures++
				continue
			}
			if matched && !force {
				writeOutput("[MATCH] %s; no write\n", p.Name)
				continue
			}
			if verify {
				writeOutput("[ERROR] %s cloud certificate differs\n", p.Name)
				failures++
				continue
			}
		} else if verify {
			writeOutput("[ERROR] read-only verification unsupported for %s\n", p.Mode)
			failures++
			continue
		}
		if err := update(p, m.cert, m.key); err != nil {
			writeOutput("[ERROR] update %s: %v\n", p.Name, err)
			failures++
			continue
		}
		if p.Mode == "alicdn" || p.Mode == "alioss" {
			matched, err := read(p, m.leaf)
			if err != nil || !matched {
				writeOutput("[ERROR] post-update verification %s failed (matching=%t): %v\n", p.Name, matched, err)
				failures++
				continue
			}
		}
		writeOutput("[UPDATED] %s\n", p.Name)
	}
	if failures != 0 {
		return fmt.Errorf("%d targets failed", failures)
	}
	return nil
}

func fingerprintMatches(value string, leaf *x509.Certificate) bool {
	v := strings.ToLower(strings.ReplaceAll(value, ":", ""))
	s1 := sha1.Sum(leaf.Raw)
	s2 := sha256.Sum256(leaf.Raw)
	return v == hex.EncodeToString(s1[:]) || v == hex.EncodeToString(s2[:])
}

func cloudMatches(p Project, leaf *x509.Certificate) (bool, error) {
	if p.Mode == "alicdn" {
		client := cdn.NewClient(p.AccessKeyID, p.AccessKeySecret)
		client.WithVersion("2018-05-10")
		var response struct {
			CertInfos struct {
				CertInfo []struct{ ServerCertificate, ServerCertificateStatus string }
			}
		}
		err := client.Invoke("DescribeDomainCertificateInfo", struct{ DomainName string }{p.Domain}, &response)
		if err != nil {
			return false, err
		}
		if len(response.CertInfos.CertInfo) != 1 {
			return false, fmt.Errorf("expected exactly one domain certificate")
		}
		info := response.CertInfos.CertInfo[0]
		if info.ServerCertificateStatus != "on" {
			return false, nil
		}
		block, _ := pem.Decode([]byte(info.ServerCertificate))
		if block == nil {
			return false, nil
		}
		return string(block.Bytes) == string(leaf.Raw), nil
	}
	client, err := oss.New(p.OssEndpoint, p.AccessKeyID, p.AccessKeySecret, oss.Timeout(15, 30))
	if err != nil {
		return false, err
	}
	data, err := client.GetBucketCname(p.OssBucket)
	if err != nil {
		return false, err
	}
	var response struct {
		Cnames []struct {
			Domain, Status string
			Certificate    struct{ Fingerprint, Status string }
		} `xml:"Cname"`
	}
	if err := xml.Unmarshal([]byte(data), &response); err != nil {
		return false, err
	}
	for _, c := range response.Cnames {
		if c.Domain == p.Domain {
			return c.Status == "Enabled" && c.Certificate.Status == "Enabled" && fingerprintMatches(c.Certificate.Fingerprint, leaf), nil
		}
	}
	return false, fmt.Errorf("existing OSS CNAME %s not found; refusing to create it", p.Domain)
}

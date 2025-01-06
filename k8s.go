package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func getClientset() (*kubernetes.Clientset, error) {
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %v", err)
	}
	return clientset, nil
}

func updateK8sSecretCert(clientset *kubernetes.Clientset, project Project, cert, key []byte) error {
	if project.K8sNamespace == "" || project.K8sSecretName == "" {
		return fmt.Errorf("kubernetes namespace, secret name must be specified")
	}

	ctx := context.TODO()
	secret, err := clientset.CoreV1().Secrets(project.K8sNamespace).Get(ctx, project.K8sSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes secret: %v", err)
	}

	secret.Data["tls.crt"] = cert
	secret.Data["tls.key"] = key

	_, err = clientset.CoreV1().Secrets(project.K8sNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update Kubernetes secret: %v", err)
	}

	fmt.Printf("[INFO] Kubernetes secret '%s' in namespace '%s' updated successfully\n", project.K8sSecretName, project.K8sNamespace)
	return nil
}

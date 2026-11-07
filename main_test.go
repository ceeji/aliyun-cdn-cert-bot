package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestDeploymentAndNotificationResults(t *testing.T) {
	for _, test := range []struct {
		name          string
		deploymentErr error
		notify        bool
		responseCode  int
		wantExitCode  int
		wantCalls     int32
	}{
		{"部署失败通知成功", errors.New("部署失败"), true, http.StatusOK, 1, 1},
		{"部署失败通知失败", errors.New("部署失败"), true, http.StatusBadGateway, 1, 1},
		{"部署成功通知失败", nil, true, http.StatusBadGateway, 1, 1},
		{"部署成功通知成功", nil, true, http.StatusOK, 0, 1},
		{"成功静默", nil, false, http.StatusOK, 0, 0},
		{"失败禁用通知", errors.New("部署失败"), false, http.StatusOK, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(test.responseCode)
				fmt.Fprint(w, `{"errcode":0}`)
			}))
			defer server.Close()
			if got := deploymentExitCode(test.deploymentErr, server.URL, test.notify); got != test.wantExitCode {
				t.Fatalf("退出码为 %d，期望 %d", got, test.wantExitCode)
			}
			if got := calls.Load(); got != test.wantCalls {
				t.Fatalf("通知次数为 %d，期望 %d", got, test.wantCalls)
			}
		})
	}
}

func TestMainDeploymentFailureExitCode(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"errcode":0}`)
	}))
	defer server.Close()
	config := Config{QiyewechatWebhookUrl: server.URL}
	data, err := yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		options   []string
		wantCalls int32
	}{
		{nil, 1},
		{[]string{"--notify-on-failure"}, 1},
		{[]string{"--notify-on-failure", "--no-notify"}, 0},
		{[]string{"--notify-on-failure", "--check"}, 0},
		{[]string{"--notify-on-failure", "--verify"}, 0},
	} {
		before := calls.Load()
		args := []string{"-test.run=^TestBotMainProcess$", "--", "--config", configPath}
		args = append(args, test.options...)
		command := exec.Command(os.Args[0], args...)
		command.Env = append(os.Environ(), "CERT_BOT_TEST_MAIN=1")
		output, err := command.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("参数 %v 应返回 1，实际为 %v，输出为 %s", test.options, err, output)
		}
		if got := calls.Load() - before; got != test.wantCalls {
			t.Fatalf("参数 %v 的通知次数为 %d，期望 %d", test.options, got, test.wantCalls)
		}
	}
}

func TestMainSuccessOnlyNotifiesWithoutFailureOption(t *testing.T) {
	var notifications atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/webhook" {
			notifications.Add(1)
			fmt.Fprint(w, `{"errcode":0}`)
			return
		}
		if request.Method != http.MethodPut {
			t.Errorf("部署请求应为 PUT，实际为 %s", request.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	project := fixture(t, "a.example.com")
	project.Mode = "apisix"
	project.ApisixAdminURL = server.URL
	project.ApisixAdminKey = "fixture"
	data, err := yaml.Marshal(Config{Projects: []Project{project}, QiyewechatWebhookUrl: server.URL + "/webhook"})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]string{{"--notify-on-failure"}, {}} {
		args := []string{"-test.run=^TestBotMainProcess$", "--", "--config", configPath}
		command := exec.Command(os.Args[0], append(args, options...)...)
		command.Env = append(os.Environ(), "CERT_BOT_TEST_MAIN=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("参数 %v 应成功，实际为 %v，输出为 %s", options, err, output)
		}
		if len(options) != 0 && notifications.Load() != 0 {
			t.Fatal("成功时不应发送失败通知")
		}
	}
	if notifications.Load() != 1 {
		t.Fatal("默认模式仍应发送成功通知")
	}
}

// TestBotMainProcess 在隔离子进程中执行真实入口，验证 os.Exit 传播部署失败。
func TestBotMainProcess(t *testing.T) {
	if os.Getenv("CERT_BOT_TEST_MAIN") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			os.Args = append(os.Args[:1], os.Args[index+1:]...)
			break
		}
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
}

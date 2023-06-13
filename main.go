package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Request struct {
	Path       string `json:"path"`
	Comment    string `json:"comment"`
	ClientIP   string `json:"clientIP"`
	Method     string `json:"method"`
	StatusCode int    `json:"statusCode"`
	Latency    string `json:"latency"`
	ReqID      string `json:"reqID"`
}

type Resp struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Bean VersionDetail `json:"bean"`
}

type VersionDetail struct {
	Version string `json:"version"`
}

func main() {
	var config *rest.Config
	// 获取Kubernetes客户端的配置
	config, err := rest.InClusterConfig()
	if err != nil {
		logrus.Error("get InClusterConfig error:", err)
	}
	if config == nil {
		config, err = clientcmd.BuildConfigFromFlags("", "/Users/qyc/go/src/awesomeProject/pod_log/kubeconfig")
		if err != nil {
			logrus.Error("Failed to build config: %v", err)
			return
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
		return
	}
	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		namespace = "helm"
	}
	var containerName string
	podName := os.Getenv("POD_NAME")
	if !strings.Contains(podName, "chartmuseum") {
		pods, err := clientset.CoreV1().Pods(namespace).List(context.Background(), v1.ListOptions{})
		if err != nil {
			logrus.Errorf("get pods name error:%v", err)
			return
		}

		for _, pod := range pods.Items {
			if strings.Contains(pod.Name, "chartmuseum") {
				podName = pod.Name
				containerName = pod.Spec.Containers[0].Name
			}
		}
	} else {
		pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), podName, v1.GetOptions{})
		if err != nil {
			logrus.Errorf("get pods name error:%v", err)
			return
		}
		podName = pod.Name
		for _, c := range pod.Spec.Containers {
			if strings.Contains(c.Name, "chartmuseum") {
				containerName = c.Name
			}
		}
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
	})

	podLogs, err := req.Stream(context.Background())
	if err != nil {
		log.Fatalf("Failed to open log stream: %v", err)
	}
	defer podLogs.Close()

	stopCh := make(chan struct{})
	go func() {
		defer close(stopCh)
		_, err = watchLogs(podLogs)
		if err != nil {
			log.Fatalf("Failed to watch logs: %v", err)
		}
	}()

	// Run for a specific duration, or until an error occurs
	duration := 24 * time.Hour
	select {
	case <-time.After(duration):
		log.Printf("Log monitoring duration reached. Exiting.")
	case <-stopCh:
		log.Printf("Log monitoring stopped due to an error.")
	}
}

func watchLogs(stream io.ReadCloser) (string, error) {
	// Read log lines from the stream
	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("failed to read log line: %v", err)
		}
		fmt.Printf("Log line: %s", line)
		var req string
		if strings.Contains(string(line), "Request served") {
			comma1 := strings.Index(string(line), "{")
			comma2 := strings.Index(string(line), "}")
			req = string(line)[comma1 : comma2+1]
		}

		var request Request
		err = json.Unmarshal([]byte(req), &request)
		if err != nil {
			continue
		}
		if strings.HasPrefix(request.Path, "/charts/") && request.StatusCode == 200 {

			// 解析出chart名字和版本 /charts/nginx-15.0.1.tgz
			chart := strings.Split(request.Path, "/")[2]
			ChartName := strings.Split(chart, "-")[0]
			temp := strings.Split(chart, "-")[1]
			ChartVersion := strings.TrimSuffix(strings.TrimSuffix(temp, ".tgz"), ".tgz.prov")
			// 调用openapi接口获取应用信息，统计安装数据
			url := os.Getenv("STORE_DOMAIN")
			marketID := os.Getenv("MARKET_ID")
			response, err := http.Get(fmt.Sprintf("%v/app-server/markets/%v/chart/%v/versions/%v/count?forInstall=true", url, marketID, ChartName, ChartVersion))
			if err != nil {
				fmt.Println("HTTP request error:", err)
			}
			defer response.Body.Close()
			chartBody, err := ioutil.ReadAll(response.Body)
			if err != nil {
				fmt.Println("Failed to read response body:", err)
			}
			var resp Resp
			_ = json.Unmarshal(chartBody, &resp)
			if resp.Code == 200 && resp.Bean.Version == ChartVersion {
				fmt.Println("Successfully counted installation data")
			}
		}
	}
	return "", nil
}

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"log"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Request   Request `json:"request"`
}

type Request struct {
	Path       string `json:"path"`
	Comment    string `json:"comment"`
	ClientIP   string `json:"clientIP"`
	Method     string `json:"method"`
	StatusCode int    `json:"statusCode"`
	Latency    string `json:"latency"`
	ReqID      string `json:"reqID"`
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
	podName := "image-demo-nginx-57d594dcd-vn9rm"
	pod, err := clientset.CoreV1().Pods(namespace).Get(context.Background(), podName, v1.GetOptions{})
	if err != nil {
		logrus.Errorf("get pod %v error:%v", podName, err)
		return
	}
	var containerName string
	containerName = "nginx"
	if strings.Contains(pod.Name, "chartmuseum") {
		//for _, c := range pod.Spec.Containers {
		//	if strings.Contains(c.Name, "chartmuseum"){
		//		containerName = c.Name
		//	}
		//}
		containerName = pod.Spec.Containers[0].Name
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
	duration := 5 * time.Minute
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
		var logEntry LogEntry
		err = json.Unmarshal(line, &logEntry)
		if err != nil {
			logrus.Errorf("Failed to parse log entry: %s\n", line)
			continue
		}
		if strings.HasPrefix(logEntry.Request.Path, "/charts/") && logEntry.Request.StatusCode == 200 {
			//TODO: 调用openapi接口获取应用信息，统计安装数据
			logrus.Info("========统计安装数据========")
		}
	}
	return "", nil
}

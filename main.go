package main

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"io"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"strings"
	"time"
)

type LogEntry struct {
	Timestamp string  `json:"timestamp"`
	Info      string  `json:"info"`
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
		return
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
		logrus.Error("Failed to create client: %v", err)
	}

	namespace := os.Getenv("NAMESPACE")
	watchPodCreation(clientset, namespace)
}

func watchPodCreation(clientset *kubernetes.Clientset, namespace string) {
	watchInterface, err := clientset.CoreV1().Pods(namespace).Watch(context.Background(), v1.ListOptions{})
	if err != nil {
		logrus.Error("Failed to watch Pods: %v", err)
	}
	for event := range watchInterface.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			logrus.Info("Failed to parse Pod event")
			continue
		}
		temp := strings.Split(pod.Name, "-")
		tempList := temp[:len(temp)-2]
		podNamePrefix := strings.Join(tempList, "-")
		containerName := pod.Spec.Containers[0].Name
		if strings.Contains(podNamePrefix, "nginx") && event.Type == watch.Added {
			logrus.Infof("New Pod detected: %s", pod.Name)
			go watchPodLogs(clientset, namespace, pod.Name, containerName)
		}
	}
}

func watchPodLogs(clientset *kubernetes.Clientset, namespace, podName, containerName string) {
	for {
		podLogs, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
			Container: containerName,
			Follow:    true,
		}).Stream(context.Background())
		if err != nil {
			logrus.Errorf("Failed to open log stream: %v", err)
		}

		reader := bufio.NewReader(podLogs)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				logrus.Infof("Failed to read log line: %v", err)
				break
			}
			log.Printf("Log line: %s", line)
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

		podLogs.Close()
		time.Sleep(5 * time.Second)
	}
}

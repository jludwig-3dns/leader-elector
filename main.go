// Package main provides a Kubernetes sidecar that performs leader election.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

var (
	statusDir     string
	leaderFile    string
	followerFile  string
	currentRole   string
	roleCancel    context.CancelFunc
)

const (
	defaultStatusDir = "/tmp/leader_status"
	tickInterval     = 5 * time.Second
)

func initStatus() {
	statusDir = os.Getenv("STATUS_DIR")
	if statusDir == "" {
		statusDir = defaultStatusDir
	}
	if err := os.MkdirAll(statusDir, 0755); err != nil {
		panic(fmt.Sprintf("failed to create status directory %q: %v", statusDir, err))
	}
	leaderFile = filepath.Join(statusDir, "leader")
	followerFile = filepath.Join(statusDir, "follower")
}

func main() {
	initStatus()
	go startHealthServer()

	// Get in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		// If in-cluster config fails, use KUBECONFIG
		kubeconfig, exists := os.LookupEnv("KUBECONFIG")
		if !exists {
			panic("Failed to get in-cluster config and KUBECONFIG not set")
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	id, err := os.Hostname()
	if err != nil {
		panic(err.Error())
	}

	// Get lease and namespace from environment variables
	leaseName, exists := os.LookupEnv("LEASE_NAME")
	if !exists {
		panic("LEASE_NAME not set")
	}

	namespace, exists := os.LookupEnv("NAMESPACE")
	if !exists {
		panic("NAMESPACE not set")
	}

	// Lock required for leader election
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	// Try and become the leader
	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				setRole("leader", id)
				<-ctx.Done()
			},
			OnStoppedLeading: func() {
				setRole("follower", id)
			},
			OnNewLeader: func(identity string) {
				if identity != id {
					setRole("follower", id)
				}
			},
		},
	})
}

func setRole(role, identity string) {
	if currentRole == role {
		return
	}

	if roleCancel != nil {
		roleCancel()
	}
	
	os.Remove(leaderFile)
	os.Remove(followerFile)

	filePath := followerFile
	if role == "leader" {
		filePath = leaderFile
	}

	if err := os.WriteFile(filePath, []byte(identity), 0644); err != nil {
		fmt.Printf("failed to write %s file: %v\n", role, err)
	}
	currentRole = role

	ctx, cancel := context.WithCancel(context.Background())
	roleCancel = cancel
	go func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				if err := os.Chtimes(filePath, now, now); err != nil {
					fmt.Printf("failed to touch %s: %v\n", filePath, err)
				}
			}
		}
	}()
}

func startHealthServer() {
	port := os.Getenv("HEALTH_PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	
	addr := fmt.Sprintf(":%s", port)
	fmt.Printf("Starting health server on %s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Printf("health server error: %v\n", err)
	}
}

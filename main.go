package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Server struct {
	clientset *kubernetes.Clientset
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("healthy"))
}

func (s *Server) proxyHandler(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		http.Error(w, "invalid host header", http.StatusBadRequest)
		return
	}

	subdomain := parts[0]
	log.Printf("Incoming request for %s", subdomain)

	_, err := s.clientset.CoreV1().
		Services(os.Getenv("POD_NAMESPACE")).
		Get(context.Background(), subdomain, v1.GetOptions{})

	if err != nil {
		log.Printf("Service %s not found: %v", subdomain, err)
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	// Create target URL
	targetURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:80", subdomain, os.Getenv("POD_NAMESPACE"))
	proxy := httputil.NewSingleHostReverseProxy(&url.URL{
		Scheme: "http",
		Host:   targetURL,
	})

	// Add error handling for the proxy
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error: %v", err)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}

	proxy.ServeHTTP(w, r)
}

func main() {
	// Create in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to create in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create clientset: %v", err)
	}

	server := &Server{
		clientset: clientset,
	}

	// Create router
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.healthHandler)
	mux.HandleFunc("/", server.proxyHandler)

	// Create server
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting server on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}

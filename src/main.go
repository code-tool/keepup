package main

import (
	"context"
	"fmt"
	"keepup/src/config"
	"keepup/src/handler"
	"keepup/src/metrics"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/go-redis/redis/v9"
)

var (
	server             *http.Server
	shutdownWaiter     sync.WaitGroup
	PackageHandler     *handler.PackageVersionsHandler
	osReleaseHandler   *handler.OsReleasesMiddleware
	kubeClusterHandler *handler.KubernetesClusterMiddleware
	buildVersion       string
)

func main() {
	ctx := context.Background()

	db, err := strconv.Atoi(config.GetConfig().REDIS_DBNO)
	if err != nil {
		log.Fatalf("Can't configure REDIS_DBNO: %v", err)
	}
	con := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", config.GetConfig().REDIS_ADDR, config.GetConfig().REDIS_PORT),
		DB:   db,
	})

	ttlSeconds, err := strconv.Atoi(config.GetConfig().TTL_SECONDS)
	if err != nil {
		log.Fatalf("Can't configure TTL_SECONDS: %v", err)
	}

	osReleaseHandler = &handler.OsReleasesMiddleware{
		OsReleases: &handler.OsReleases{
			Items: make(map[uuid.UUID]handler.OsRelease),
		},
		Context:  ctx,
		Client:   con,
		ApiToken: config.GetConfig().API_TOKEN,
		TTL:      ttlSeconds,
	}

	PackageHandler = &handler.PackageVersionsHandler{
		PackageVersions: &handler.PackageVersionss{
			Items: make(map[uuid.UUID]handler.PackageVersions),
		},
		Client:   con,
		Context:  ctx,
		ApiToken: config.GetConfig().API_TOKEN,
		TTL:      ttlSeconds,
	}

	kubeClusterHandler = &handler.KubernetesClusterMiddleware{
		Clusters: &handler.KubernetesClusters{
			Items: make(map[uuid.UUID]handler.KubernetesCluster),
		},
		Context:  ctx,
		Client:   con,
		ApiToken: config.GetConfig().API_TOKEN,
		TTL:      ttlSeconds,
	}

	osReleaseCollector := metrics.OsReleaseCollector{
		RelInfo: osReleaseHandler,
	}
	packageCollector := metrics.PackageVersionsCollector{
		PackageInfo: PackageHandler,
	}

	HelmCollector := metrics.KubernetesClusterCollector{
		ClusterInfo: kubeClusterHandler,
	}

	prometheus.MustRegister(packageCollector)
	prometheus.MustRegister(osReleaseCollector)
	prometheus.MustRegister(HelmCollector)

	shutdownWaiter.Add(1)
	configureServer()
	initSignalHandler()
	initRouting()
	startServer()
	shutdownWaiter.Wait()
	log.Println("Exiting server")
}

func configureServer() {
	log.Printf("Creating server on port %s.", config.GetConfig().LISTEN_PORT)
	server = &http.Server{
		Addr: fmt.Sprintf(":%s", config.GetConfig().LISTEN_PORT),
	}
	server.RegisterOnShutdown(func() {
		log.Println("Shutting down server.")
		handler.FlushBufferOnShutdown(&shutdownWaiter)
	})
}

func initSignalHandler() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGKILL)
	go handleSignal(server, sigChan)
}

func initRouting() {
	http.Handle("/metrics", promhttp.Handler())
	//http.HandleFunc("/static/puppet.tar.bz2", osReleaseHandler.HandlePuppet)
	//http.HandleFunc("/static/ansible.tar.bz2", osReleaseHandler.HandleAnsible)
	http.HandleFunc("/os-release", osReleaseHandler.HandleOsRelease)
	http.HandleFunc("/package-version", PackageHandler.HandlePackage)
	http.HandleFunc("/healthcheck", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
}

func startServer() {
	log.Printf("Starting http server. Build [%s]", buildVersion)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server ListenAndServe: %v.", err)
		shutdownWaiter.Done()
	}
}

func handleSignal(server *http.Server, signals <-chan os.Signal) {
	log.Println("Listening OS signals.")
	for sig := range signals {
		log.Printf("OS signal received [%s].", sig.String())
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatalf("HTTP server Shutdown: %v.", err)
		}
	}
}

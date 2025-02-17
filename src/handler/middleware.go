package handler

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-redis/redis/v9"
	"github.com/google/uuid"
)

type PackageVersionsHandler struct {
	PackageVersions *PackageVersionss
	Client          *redis.Client
	Context         context.Context
	ApiToken        string
	TTL             int
}
type PackageDocument struct {
	Packages map[string]string `json:"packages"`
}

type ResponseDocument struct {
	ID       uuid.UUID                `json:"id"`
	Packages map[string]PackageDetail `json:"packages"`
}

type IDDocumentPackage struct {
	ID uuid.UUID `json:"id"`
}

type OsReleasesMiddleware struct {
	OsReleases *OsReleases
	Client     *redis.Client
	Context    context.Context
	ApiToken   string
	TTL        int
}

type ReleaseDocument struct {
	OsRelease OsRelease `json:"release"`
}

type IDDocument struct {
	ID uuid.UUID `json:"id"`
}

type KubernetesClusterMiddleware struct {
	Clusters *KubernetesClusters
	Client   *redis.Client
	Context  context.Context
	ApiToken string
	TTL      int
}

type ClusterDocument struct {
	Cluster KubernetesCluster `json:"cluster"`
}

type IDClusterDocument struct {
	ID uuid.UUID `json:"id"`
}

func (s *OsReleasesMiddleware) HandleOsRelease(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("x-api-token")
	if token != s.ApiToken {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "FORBIDDEN")
		return
	} else {
		switch strings.ToUpper(r.Method) {
		case "GET":
			w.Header().Set("Content-Type", "application/json")
			s.handleGetByID(w, r)
		case "PUT":
			w.Header().Set("Content-Type", "application/json")
			s.handleInsert(w, r)
		}
	}
}

func (s *OsReleasesMiddleware) handleInsert(w http.ResponseWriter, r *http.Request) {
	var req ReleaseDocument
	var res IDDocument
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, e := s.OsReleases.Insert(req.OsRelease, s.Context, s.Client, s.TTL)
	if e != nil {
		http.Error(w, e.Error(), http.StatusBadRequest)
		return
	}
	res = IDDocument{ID: id}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

}

func (s *OsReleasesMiddleware) handleGetByID(w http.ResponseWriter, r *http.Request) {
	var req IDDocument
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rel, err := s.OsReleases.Retrieve(req.ID, s.Context, s.Client)
	if err == ErrIDNotFound {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res := ReleaseDocument{OsRelease: rel}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *OsReleasesMiddleware) HandlePuppet(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/octet-stream")
	w.Header().Add("Content-Transfer-Encoding", "binary")
	w.Header().Add("Cache-Control", "private")
	w.Header().Add("Content-Disposition", "attachment; filename=puppet.tar.bz2")
	http.ServeFile(w, r, "./static/puppet.tar.bz2")
}

func (s *OsReleasesMiddleware) HandleAnsible(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/octet-stream")
	w.Header().Add("Content-Transfer-Encoding", "binary")
	w.Header().Add("Cache-Control", "private")
	w.Header().Add("Content-Disposition", "attachment; filename=ansible.tar.bz2")
	http.ServeFile(w, r, "./static/ansible.tar.bz2")
}

func (s *OsReleasesMiddleware) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "OK")
}

func FlushBufferOnShutdown(shutdownWaiter *sync.WaitGroup) {
	// TODO: cleanup logic
	shutdownWaiter.Done()
}

func (p *PackageVersionsHandler) handleInsertPackages(w http.ResponseWriter, r *http.Request) {
	var req PackageDocument
	var res IDDocumentPackage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	cleanedPackages := make(map[string]string)
	for key, value := range req.Packages {
		if key != "host_ip" && key != "data_center" {
			cleanedPackages[key] = value
		}
	}

	convertedPackages := make(map[string]PackageDetail)
	for name, version := range cleanedPackages {
		convertedPackages[name] = PackageDetail{
			CurrentVersion: version,
		}
	}

	pkg := PackageVersions{
		DataCenterPkg: req.Packages["data_center"],
		HostIPPkg:     req.Packages["host_ip"],
		Packages:      convertedPackages,
	}

	ttl := p.TTL

	id, err := p.PackageVersions.Insert(pkg, p.Context, p.Client, func(packageName string) (string, string, error) {
		return queryEndOfLifeAPI(packageName, p.Context, p.Client)
	}, ttl)

	if err != nil {
		log.Printf("Failed to insert packages: %v", err)
		http.Error(w, "Failed to insert package data", http.StatusInternalServerError)
		return
	}

	res = IDDocumentPackage{ID: id}
	err = json.NewEncoder(w).Encode(res)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (p *PackageVersionsHandler) handleGetPackages(w http.ResponseWriter, r *http.Request) {
	var req IDDocumentPackage

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pkg, err := p.PackageVersions.Retrieve(req.ID, p.Context, p.Client)
	if err == ErrIDNotFoundPackage {
		http.Error(w, "Packages data not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Failed to retrieve packages data", http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(ResponseDocument{
		ID:       pkg.IDPkg,
		Packages: pkg.Packages,
	})
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (s *PackageVersionsHandler) HandlePackage(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("x-api-token")
	if token != s.ApiToken {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "FORBIDDEN")
		return
	} else {
		switch strings.ToUpper(r.Method) {
		case "GET":
			w.Header().Set("Content-Type", "application/json")
			s.handleGetPackages(w, r)
		case "PUT":
			w.Header().Set("Content-Type", "application/json")
			s.handleInsertPackages(w, r)
		}
	}
}

func (s *KubernetesClusterMiddleware) HandleKubernetesCluster(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("x-api-token")
	if token != s.ApiToken {
		http.Error(w, "FORBIDDEN", http.StatusForbidden)
		return
	}

	switch strings.ToUpper(r.Method) {
	case "GET":
		w.Header().Set("Content-Type", "application/json")
		s.handleGetClusterByID(w, r)
	case "PUT":
		w.Header().Set("Content-Type", "application/json")
		s.handleInsertCluster(w, r)
	}
}

func (s *KubernetesClusterMiddleware) handleInsertCluster(w http.ResponseWriter, r *http.Request) {
	var cluster KubernetesCluster
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Println("Failed to read request body:", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	log.Printf("Received JSON: %s", string(body))
	if err := json.Unmarshal(body, &cluster); err != nil {
		log.Println("Failed to parse JSON:", err)
		http.Error(w, "Invalid JSON format", http.StatusBadRequest)
		return
	}

	id, err := s.Clusters.InsertClusterData(cluster, s.Context, s.Client, s.TTL)
	if err != nil {
		log.Println("Failed to insert cluster:", err)
		http.Error(w, "Failed to store data", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(IDClusterDocument{ID: id})
	log.Printf("Cluster stored with ID: %s", id)
}

func (s *KubernetesClusterMiddleware) handleGetClusterByID(w http.ResponseWriter, r *http.Request) {
	var req IDClusterDocument

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid JSON request", http.StatusBadRequest)
		return
	}

	cluster, err := s.Clusters.RetrieveCluster(req.ID, s.Context, s.Client)
	if err == ErrClusterNotFound {
		http.Error(w, "Cluster not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	res := ClusterDocument{Cluster: cluster}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(res); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

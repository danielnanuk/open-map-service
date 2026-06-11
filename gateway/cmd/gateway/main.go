package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/danielnanuk/open-map-service/gateway/internal/httpapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/search"
	"github.com/danielnanuk/open-map-service/gateway/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	osURL := env("OPENSEARCH_URL", "http://localhost:9200")
	dsn := env("DATABASE_URL", "postgresql://places:places@localhost:5432/places")
	port := env("PORT", "8080")

	pg, err := store.NewPG(context.Background(), dsn)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	h := httpapi.New(search.New(osURL), pg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/places:searchText", h.SearchText)
	mux.HandleFunc("POST /v1/places:searchNearby", h.SearchNearby)
	mux.HandleFunc("POST /v1/places:autocomplete", h.Autocomplete)
	mux.HandleFunc("GET /v1/places/{id}", h.GetPlace)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Printf("gateway listening on :%s", port)
	// TODO(M5): 换成 http.Server{ReadHeaderTimeout,...} + SIGTERM 优雅退出(生产加固里程碑)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

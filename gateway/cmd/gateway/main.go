package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/danielnanuk/open-map-service/gateway/internal/geocode"
	"github.com/danielnanuk/open-map-service/gateway/internal/httpapi"
	"github.com/danielnanuk/open-map-service/gateway/internal/route"
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
	nmURL := env("NOMINATIM_URL", "http://localhost:8081")
	vhURL := env("VALHALLA_URL", "http://localhost:8002")
	port := env("PORT", "8080")

	pg, err := store.NewPG(context.Background(), dsn)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	osrmRouters := map[string]httpapi.MatrixRouter{
		"auto":          route.NewOSRM(env("OSRM_CAR_URL", "http://localhost:5000")),
		"motor_scooter": route.NewOSRM(env("OSRM_MOTO_URL", "http://localhost:5001")),
		"tuktuk":        route.NewOSRM(env("OSRM_TUKTUK_URL", "http://localhost:5002")),
	}
	vh := route.New(vhURL)
	h := httpapi.NewWithGeocoder(search.New(osURL), pg, geocode.NewNominatim(nmURL)).
		WithRouter(vh).
		WithMatrix(osrmRouters, vh)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/places:searchText", h.SearchText)
	mux.HandleFunc("POST /v1/places:searchNearby", h.SearchNearby)
	mux.HandleFunc("POST /v1/places:autocomplete", h.Autocomplete)
	mux.HandleFunc("GET /v1/places/{id}", h.GetPlace)
	mux.HandleFunc("GET /maps/api/geocode/json", h.Geocode)
	mux.HandleFunc("POST /directions/v2:computeRoutes", h.ComputeRoutes)
	mux.HandleFunc("POST /distanceMatrix/v2:computeRouteMatrix", h.ComputeRouteMatrix)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Printf("gateway listening on :%s", port)
	// TODO(M5): 换成 http.Server{ReadHeaderTimeout,...} + SIGTERM 优雅退出(生产加固里程碑)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

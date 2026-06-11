package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danielnanuk/open-map-service/gateway/internal/auth"
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

	authEnabled := env("AUTH_ENABLED", "false") == "true"
	keyStore := auth.NewStore(pg, time.Minute)
	handler := httpapi.WithRequestID(httpapi.WithAuth(authEnabled, keyStore)(mux))
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second, // 大矩阵响应(25 万元素 ~28MB)需要余量
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("gateway listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

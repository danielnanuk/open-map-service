package store

type PlaceRow struct {
	PlaceID      string
	Names        map[string]string
	Categories   []string
	Phone        string
	Website      string
	OpeningHours string
	Address      map[string]string
	Lon, Lat     float64
}

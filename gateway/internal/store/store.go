// Package store 提供 places 权威库(PostGIS)的详情读取。
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

type PG struct{ pool *pgxpool.Pool }

func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg ping: %w", err)
	}
	return &PG{pool: pool}, nil
}

// GetPlace 返回 (nil, nil) 表示不存在。
func (p *PG) GetPlace(ctx context.Context, id string) (*PlaceRow, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT place_id, names, categories,
		       COALESCE(phone,''), COALESCE(website,''), COALESCE(opening_hours,''),
		       COALESCE(address, '{}'::jsonb), ST_X(geom), ST_Y(geom)
		FROM places WHERE place_id = $1`, id)
	var out PlaceRow
	err := row.Scan(&out.PlaceID, &out.Names, &out.Categories,
		&out.Phone, &out.Website, &out.OpeningHours, &out.Address, &out.Lon, &out.Lat)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// LookupKey 实现 auth.KeyDB。
func (p *PG) LookupKey(ctx context.Context, key string) (int, bool, error) {
	var rpm int
	err := p.pool.QueryRow(ctx,
		`SELECT rpm_limit FROM api_keys WHERE key = $1 AND active`, key).Scan(&rpm)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return rpm, true, nil
}

// GetNearbyPlaces 返回距 (lat,lon) 半径 radiusM 米内最近的 limit 个地点(近→远)。
func (p *PG) GetNearbyPlaces(ctx context.Context, lat, lon, radiusM float64, limit int) ([]PlaceRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT place_id, names, categories,
		       COALESCE(phone,''), COALESCE(website,''), COALESCE(opening_hours,''),
		       COALESCE(address, '{}'::jsonb), ST_X(geom), ST_Y(geom)
		FROM places
		WHERE ST_DWithin(geom::geography, ST_SetSRID(ST_MakePoint($2, $1), 4326)::geography, $3)
		ORDER BY geom <-> ST_SetSRID(ST_MakePoint($2, $1), 4326)
		LIMIT $4`, lat, lon, radiusM, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlaceRow
	for rows.Next() {
		var r PlaceRow
		if err := rows.Scan(&r.PlaceID, &r.Names, &r.Categories,
			&r.Phone, &r.Website, &r.OpeningHours, &r.Address, &r.Lon, &r.Lat); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

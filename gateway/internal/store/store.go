// Package store 提供 places 权威库(PostGIS)的详情读取。
package store

import (
	"context"
	"errors"

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

type PG struct{ Pool *pgxpool.Pool }

func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &PG{Pool: pool}, nil
}

// GetPlace 返回 (nil, nil) 表示不存在。
func (p *PG) GetPlace(ctx context.Context, id string) (*PlaceRow, error) {
	row := p.Pool.QueryRow(ctx, `
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

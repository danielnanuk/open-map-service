CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS places (
  place_id       text PRIMARY KEY,            -- Overture GERS id 或 osm:<type>:<id>
  primary_source text NOT NULL,               -- 'overture' | 'osm'
  names          jsonb NOT NULL,              -- {"default":..,"km":..,"en":..,"zh":..}
  categories     text[] NOT NULL DEFAULT '{point_of_interest}',
  raw_category   text,
  phone          text,
  website        text,
  opening_hours  text,                        -- OSM opening_hours 原文
  address        jsonb,                       -- {"freeform":..,"locality":..,"region":..,"country":..}
  confidence     real,
  sources        jsonb NOT NULL DEFAULT '[]',
  geom           geometry(Point, 4326) NOT NULL,
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS places_geom_idx ON places USING GIST (geom);

CREATE TABLE IF NOT EXISTS staging_overture (
  place_id text PRIMARY KEY,
  name_default text, name_km text, name_en text, name_zh text,
  raw_category text, google_type text,
  phone text, website text,
  addr_freeform text, addr_locality text, addr_region text, addr_country text,
  confidence real, sources jsonb,
  lon double precision, lat double precision
);

CREATE TABLE IF NOT EXISTS osm_pois (
  osm_id text PRIMARY KEY,                    -- osm:node:123 / osm:way:456 / osm:rel:789
  name_default text, name_km text, name_en text, name_zh text,
  google_type text,
  phone text, website text, opening_hours text,
  lon double precision, lat double precision,
  geom geometry(Point, 4326)
);
CREATE INDEX IF NOT EXISTS osm_pois_geom_idx ON osm_pois USING GIST (geom);
-- geography 函数索引:conflation 的 ST_DWithin(geom::geography, ...) 依赖它,否则全表扫描
CREATE INDEX IF NOT EXISTS places_geog_idx   ON places   USING GIST ((geom::geography));
CREATE INDEX IF NOT EXISTS osm_pois_geog_idx ON osm_pois USING GIST ((geom::geography));

CREATE TABLE IF NOT EXISTS country_boundary (
  iso text PRIMARY KEY,
  geom geometry(MultiPolygon, 4326) NOT NULL  -- 仅接受多边形:写入方需先 ST_CollectionExtract(..., 3)
);

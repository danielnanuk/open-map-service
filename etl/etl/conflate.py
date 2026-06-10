"""OSM POI 与 Overture places 的合并去重。运行于 load 之后、index 之前。"""
import psycopg

_MATCH_SQL = """
CREATE TEMP TABLE _matches AS
SELECT o.osm_id, p.place_id,
       ST_Distance(o.geom::geography, p.geom::geography) AS dist,
       ROW_NUMBER() OVER (PARTITION BY o.osm_id
                          ORDER BY ST_Distance(o.geom::geography, p.geom::geography),
                                   p.place_id) AS rn
FROM osm_pois o
JOIN places p ON p.primary_source = 'overture'
            AND ST_DWithin(o.geom::geography, p.geom::geography, 50)
WHERE (
        -- 匹配只锚定 Overture 原生字段(default 不会被 merge 改写):任何分支在重跑时只增不减,保证幂等
        word_similarity(lower(o.name_default), lower(p.names->>'default')) > 0.45
     OR word_similarity(lower(COALESCE(o.name_en, o.name_default)),
                        lower(p.names->>'default')) > 0.45
     OR (o.name_km IS NOT NULL AND o.name_km = p.names->>'km')
      )
  AND (o.google_type = 'point_of_interest' OR p.categories[1] = 'point_of_interest'
       OR o.google_type = p.categories[1])
"""

_MERGE_SQL = """
WITH agg AS (
  -- 同一地点可能被多个 OSM 节点匹配(rn=1 扇入):全部聚合,字段按距离就近优先,来源全部记录
  SELECT m.place_id,
         (array_agg(o.phone         ORDER BY m.dist) FILTER (WHERE o.phone IS NOT NULL))[1] AS phone,
         (array_agg(o.website       ORDER BY m.dist) FILTER (WHERE o.website IS NOT NULL))[1] AS website,
         (array_agg(o.opening_hours ORDER BY m.dist) FILTER (WHERE o.opening_hours IS NOT NULL))[1] AS opening_hours,
         (array_agg(o.name_km ORDER BY m.dist) FILTER (WHERE o.name_km IS NOT NULL))[1] AS name_km,
         (array_agg(o.name_en ORDER BY m.dist) FILTER (WHERE o.name_en IS NOT NULL))[1] AS name_en,
         (array_agg(o.name_zh ORDER BY m.dist) FILTER (WHERE o.name_zh IS NOT NULL))[1] AS name_zh,
         jsonb_agg(jsonb_build_object('dataset', 'osm', 'record_id', o.osm_id) ORDER BY m.dist) AS osm_sources
  FROM _matches m
  JOIN osm_pois o ON o.osm_id = m.osm_id
  WHERE m.rn = 1
  GROUP BY m.place_id
)
UPDATE places p SET
  phone         = COALESCE(p.phone, a.phone),
  website       = COALESCE(p.website, a.website),
  opening_hours = COALESCE(p.opening_hours, a.opening_hours),
  names = p.names || jsonb_strip_nulls(jsonb_build_object(
            'km', COALESCE(p.names->>'km', a.name_km),
            'en', COALESCE(p.names->>'en', a.name_en),
            'zh', COALESCE(p.names->>'zh', a.name_zh))),
  sources = p.sources || COALESCE(
              (SELECT jsonb_agg(s) FROM jsonb_array_elements(a.osm_sources) AS s
               WHERE NOT p.sources @> jsonb_build_array(s)),
              '[]'::jsonb),
  updated_at = now()
FROM agg a
WHERE p.place_id = a.place_id
"""

_INSERT_SQL = """
INSERT INTO places (place_id, primary_source, names, categories, phone, website, opening_hours, sources, geom)
SELECT o.osm_id, 'osm',
       jsonb_strip_nulls(jsonb_build_object('default', o.name_default, 'km', o.name_km,
                                            'en', o.name_en, 'zh', o.name_zh)),
       ARRAY[o.google_type], o.phone, o.website, o.opening_hours,
       jsonb_build_array(jsonb_build_object('dataset', 'osm', 'record_id', o.osm_id)),
       o.geom
FROM osm_pois o
WHERE NOT EXISTS (SELECT 1 FROM _matches m WHERE m.osm_id = o.osm_id)
ON CONFLICT (place_id) DO UPDATE SET
  categories = EXCLUDED.categories,
  names = EXCLUDED.names, phone = EXCLUDED.phone, website = EXCLUDED.website,
  opening_hours = EXCLUDED.opening_hours, geom = EXCLUDED.geom, updated_at = now()
"""

def conflate(conn: psycopg.Connection) -> tuple[int, int]:
    with conn.transaction():
        cur = conn.cursor()
        cur.execute("DROP TABLE IF EXISTS _matches")
        cur.execute(_MATCH_SQL)
        merged = cur.execute(_MERGE_SQL).rowcount
        inserted = cur.execute(_INSERT_SQL).rowcount
        cur.execute("DROP TABLE _matches")
        return merged, inserted

# etl/etl/categories.py
"""Overture/OSM 类目 → Google place type 的最小映射。未知类目兜底 point_of_interest。

类型值必须属于 VALID_GOOGLE_TYPES(Google Places API New 的 Table A/B 子集),
否则客户端无法按 includedTypes 过滤。柬埔寨佛寺映射到 Table A 的 buddhist_temple
(place_of_worship 在 Table B,不可作过滤条件)。
"""

VALID_GOOGLE_TYPES = frozenset({
    "restaurant", "cafe", "bar", "lodging", "supermarket", "convenience_store",
    "shopping_mall", "market", "hospital", "pharmacy", "school", "university",
    "bank", "atm", "gas_station", "airport", "tourist_attraction",
    "place_of_worship", "buddhist_temple", "mosque", "church", "hindu_temple",
    "museum", "police", "cell_phone_store", "point_of_interest",
})

OVERTURE_TO_GOOGLE = {
    "restaurant": "restaurant", "fast_food_restaurant": "restaurant",
    "cafe": "cafe", "coffee_shop": "cafe",
    "bar": "bar", "pub": "bar",
    "hotel": "lodging", "motel": "lodging", "hostel": "lodging",
    "guest_house": "lodging", "bed_and_breakfast": "lodging", "resort": "lodging",
    "supermarket": "supermarket", "grocery_store": "supermarket",
    "convenience_store": "convenience_store",
    "shopping_center": "shopping_mall", "shopping_mall": "shopping_mall",
    "market": "market", "hospital": "hospital", "clinic": "hospital",
    "pharmacy": "pharmacy", "school": "school", "university": "university",
    "bank": "bank", "atm": "atm", "atms": "atm",
    "gas_station": "gas_station", "airport": "airport",
    "tourist_attraction": "tourist_attraction", "landmark_and_historical_building": "tourist_attraction",
    "buddhist_temple": "buddhist_temple", "pagoda": "buddhist_temple",
    "church_cathedral": "church", "mosque": "mosque", "hindu_temple": "hindu_temple",
    "museum": "museum", "police_station": "police",
}

_SUFFIX_RULES = [
    ("_restaurant", "restaurant"), ("_cafe", "cafe"), ("_bar", "bar"),
    ("_hotel", "lodging"), ("_school", "school"), ("_market", "market"),
    ("_temple", "buddhist_temple"), ("_hospital", "hospital"), ("_museum", "museum"),
]

def overture_to_google(primary: str | None) -> str:
    if not primary:
        return "point_of_interest"
    if primary in OVERTURE_TO_GOOGLE:
        return OVERTURE_TO_GOOGLE[primary]
    for suffix, gtype in _SUFFIX_RULES:
        if primary.endswith(suffix):
            return gtype
    return "point_of_interest"

# POI 判定:出现这些 key 之一即视为 POI(还需有 name,由 osm.py 把关)。
# tourism 在 amenity 之前:tourism=hotel + amenity=restaurant 的对象主体是酒店。
_POI_KEYS = ("tourism", "amenity", "shop", "leisure", "office", "craft", "healthcare", "aeroway")

OSM_TAG_TO_GOOGLE = {
    ("amenity", "restaurant"): "restaurant", ("amenity", "fast_food"): "restaurant",
    ("amenity", "cafe"): "cafe", ("amenity", "bar"): "bar", ("amenity", "pub"): "bar",
    ("amenity", "hospital"): "hospital", ("amenity", "clinic"): "hospital",
    ("amenity", "pharmacy"): "pharmacy", ("amenity", "school"): "school",
    ("amenity", "university"): "university", ("amenity", "bank"): "bank",
    ("amenity", "atm"): "atm", ("amenity", "fuel"): "gas_station",
    ("amenity", "place_of_worship"): "place_of_worship", ("amenity", "marketplace"): "market",
    ("amenity", "police"): "police",
    ("tourism", "hotel"): "lodging", ("tourism", "guest_house"): "lodging",
    ("tourism", "hostel"): "lodging", ("tourism", "attraction"): "tourist_attraction",
    ("tourism", "museum"): "museum",
    ("shop", "supermarket"): "supermarket", ("shop", "convenience"): "convenience_store",
    ("shop", "mall"): "shopping_mall", ("shop", "mobile_phone"): "cell_phone_store",
    ("aeroway", "aerodrome"): "airport",
}

# OSM 礼拜场所按 religion 细化为 Table A 可过滤类型
_RELIGION_TO_GOOGLE = {
    "buddhist": "buddhist_temple",
    "christian": "church",
    "muslim": "mosque",
    "hindu": "hindu_temple",
}

def osm_to_google(tags: dict) -> str | None:
    """返回 Google type;None 表示非 POI(调用方跳过)。
    取第一个有显式映射的 (key, value);全部无映射但存在 POI key 时兜底 point_of_interest。"""
    fallback = None
    for key in _POI_KEYS:
        value = tags.get(key)
        if not value:
            continue
        mapped = OSM_TAG_TO_GOOGLE.get((key, value))
        if mapped == "place_of_worship":
            mapped = _RELIGION_TO_GOOGLE.get(tags.get("religion", ""), "place_of_worship")
        if mapped:
            return mapped
        fallback = "point_of_interest"
    return fallback

"""Overture/OSM 类目 → Google place type 的最小映射。未知类目兜底 point_of_interest。"""

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
    "buddhist_temple": "place_of_worship", "pagoda": "place_of_worship",
    "church_cathedral": "place_of_worship", "mosque": "place_of_worship",
}

_SUFFIX_RULES = [
    ("_restaurant", "restaurant"), ("_cafe", "cafe"), ("_bar", "bar"),
    ("_hotel", "lodging"), ("_school", "school"), ("_market", "market"),
    ("_temple", "place_of_worship"), ("_hospital", "hospital"),
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

# POI 判定:出现这些 key 之一即视为 POI(还需有 name,由 osm.py 把关)
_POI_KEYS = ("amenity", "shop", "tourism", "leisure", "office", "craft", "healthcare", "aeroway")

OSM_TAG_TO_GOOGLE = {
    ("amenity", "restaurant"): "restaurant", ("amenity", "fast_food"): "restaurant",
    ("amenity", "cafe"): "cafe", ("amenity", "bar"): "bar", ("amenity", "pub"): "bar",
    ("amenity", "hospital"): "hospital", ("amenity", "clinic"): "hospital",
    ("amenity", "pharmacy"): "pharmacy", ("amenity", "school"): "school",
    ("amenity", "university"): "university", ("amenity", "bank"): "bank",
    ("amenity", "atm"): "atm", ("amenity", "fuel"): "gas_station",
    ("amenity", "place_of_worship"): "place_of_worship", ("amenity", "marketplace"): "market",
    ("tourism", "hotel"): "lodging", ("tourism", "guest_house"): "lodging",
    ("tourism", "hostel"): "lodging", ("tourism", "attraction"): "tourist_attraction",
    ("shop", "supermarket"): "supermarket", ("shop", "convenience"): "convenience_store",
    ("shop", "mall"): "shopping_mall", ("aeroway", "aerodrome"): "airport",
}

def osm_to_google(tags: dict) -> str | None:
    """返回 Google type;返回 None 表示这不是 POI(调用方应跳过)。"""
    for key in _POI_KEYS:
        value = tags.get(key)
        if value:
            return OSM_TAG_TO_GOOGLE.get((key, value), "point_of_interest")
    return None

from etl.categories import (OVERTURE_TO_GOOGLE, OSM_TAG_TO_GOOGLE, VALID_GOOGLE_TYPES,
                            _SUFFIX_RULES, osm_to_google, overture_to_google)

def test_overture_known_leaf():
    assert overture_to_google("restaurant") == "restaurant"
    assert overture_to_google("hotel") == "lodging"
    assert overture_to_google("buddhist_temple") == "buddhist_temple"

def test_overture_suffix_fallback():
    assert overture_to_google("cambodian_restaurant") == "restaurant"
    assert overture_to_google("boutique_hotel") == "lodging"

def test_overture_unknown_and_none():
    assert overture_to_google("weird_thing") == "point_of_interest"
    assert overture_to_google(None) == "point_of_interest"

def test_osm_mapping():
    assert osm_to_google({"amenity": "restaurant", "name": "x"}) == "restaurant"
    assert osm_to_google({"tourism": "guest_house"}) == "lodging"
    assert osm_to_google({"shop": "weird"}) == "point_of_interest"
    assert osm_to_google({"building": "yes"}) is None  # 非 POI

def test_osm_hotel_with_restaurant_is_lodging():
    assert osm_to_google({"tourism": "hotel", "amenity": "restaurant"}) == "lodging"

def test_osm_unmapped_value_does_not_shadow_mapped_key():
    assert osm_to_google({"tourism": "yes", "amenity": "bank"}) == "bank"

def test_osm_worship_refined_by_religion():
    assert osm_to_google({"amenity": "place_of_worship", "religion": "buddhist"}) == "buddhist_temple"
    assert osm_to_google({"amenity": "place_of_worship"}) == "place_of_worship"

def test_all_mapped_values_are_valid_google_types():
    values = (set(OVERTURE_TO_GOOGLE.values()) | set(OSM_TAG_TO_GOOGLE.values())
              | {g for _, g in _SUFFIX_RULES})
    assert values <= VALID_GOOGLE_TYPES

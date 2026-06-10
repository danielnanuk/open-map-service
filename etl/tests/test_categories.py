from etl.categories import overture_to_google, osm_to_google

def test_overture_known_leaf():
    assert overture_to_google("restaurant") == "restaurant"
    assert overture_to_google("hotel") == "lodging"
    assert overture_to_google("buddhist_temple") == "place_of_worship"

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

package route

import "testing"

func TestTravelModeToCosting(t *testing.T) {
	cases := []struct {
		mode, profile string
		wantCosting   string
		wantErr       bool
	}{
		{"DRIVE", "", "auto", false},
		{"", "", "auto", false}, // 默认 DRIVE
		{"WALK", "", "pedestrian", false},
		{"BICYCLE", "", "bicycle", false},
		{"TWO_WHEELER", "", "motor_scooter", false},
		{"TWO_WHEELER", "tuktuk", "motor_scooter", false},
		{"TRANSIT", "", "", true},
		{"DRIVE", "tuktuk", "", true},
		{"TWO_WHEELER", "cargo_bike", "", true}, // 未知扩展 profile
	}
	for _, c := range cases {
		costing, opts, err := TravelModeToCosting(c.mode, c.profile)
		if (err != nil) != c.wantErr {
			t.Fatalf("%s/%s: err=%v", c.mode, c.profile, err)
		}
		if err == nil && costing != c.wantCosting {
			t.Fatalf("%s/%s: costing=%s", c.mode, c.profile, costing)
		}
		if c.profile == "tuktuk" && err == nil {
			mo, ok := opts["motor_scooter"].(map[string]any)
			if !ok || mo["top_speed"] != 40 || mo["use_highways"] != 0.1 {
				t.Fatalf("tuktuk options: %v", opts)
			}
		}
	}
}

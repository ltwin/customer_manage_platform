package creativescene

import (
	"math"
	"testing"
	"time"
)

// W3: the moon reference the assistant reads (moon.go) against the
// frontend's astronomy (world/astro.ts moonTimes, astronomy-engine), which
// is checked against JPL Horizons (frontend/scripts/fixtures/astro).

type moonFrontendCase struct {
	Location         string  `json:"location"`
	LatitudeDeg      float64 `json:"latitude_deg"`
	LongitudeDeg     float64 `json:"longitude_deg"`
	Timezone         string  `json:"timezone"`
	Date             string  `json:"date"`
	LocalTime        string  `json:"local_time"`
	ElevationDeg     float64 `json:"elevation_deg"`
	CompassDeg       float64 `json:"compass_deg"`
	Phase            string  `json:"phase"`
	Illuminated      float64 `json:"illuminated"`
	Moonrise         string  `json:"moonrise"`
	Moonset          string  `json:"moonset"`
	PreviousFullMoon string  `json:"previousFullMoon"`
	NextFullMoon     string  `json:"nextFullMoon"`
	PreviousNewMoon  string  `json:"previousNewMoon"`
	NextNewMoon      string  `json:"nextNewMoon"`
}

func TestMoonReportMatchesTheFrontend(t *testing.T) {
	var data struct {
		Cases []moonFrontendCase `json:"cases"`
	}
	readJSON(t, "moon_frontend.json", &data)
	worstAngle, worstMinutes := 0.0, 0
	for _, c := range data.Cases {
		r := moonReport(physical(map[string]any{"date": c.Date, "local_time": c.LocalTime, "latitude_deg": c.LatitudeDeg, "longitude_deg": c.LongitudeDeg, "timezone": c.Timezone}))
		name := c.Location + " " + c.Date
		if r == nil {
			t.Fatalf("%s: no report", name)
		}
		if r.Phase != c.Phase {
			t.Errorf("%s: phase %s, the frontend's %s", name, r.Phase, c.Phase)
		}
		if math.Abs(r.Illumination-c.Illuminated) > 0.02 {
			t.Errorf("%s: illumination %.3f, the frontend's %.3f", name, r.Illumination, c.Illuminated)
		}
		if c.ElevationDeg > 0 {
			el1, el2, az := deg2rad(r.ElevationDeg), deg2rad(c.ElevationDeg), deg2rad(r.CompassDeg-c.CompassDeg)
			angle := rad2deg(math.Acos(math.Min(1, math.Sin(el1)*math.Sin(el2)+math.Cos(el1)*math.Cos(el2)*math.Cos(az))))
			worstAngle = math.Max(worstAngle, angle)
			// The report rounds to 0.1°.
			if angle > 0.3 {
				t.Errorf("%s: %.2f° from the frontend's moon", name, angle)
			}
		}
		for _, p := range []struct{ what, got, want string }{{"moonrise", r.Moonrise, c.Moonrise}, {"moonset", r.Moonset, c.Moonset}} {
			if (p.got == "") != (p.want == "") {
				t.Errorf("%s: %s %q, the frontend's %q", name, p.what, p.got, p.want)
				continue
			}
			if p.got != "" {
				d := minutes(t, p.got) - minutes(t, p.want)
				if d < 0 {
					d = -d
				}
				worstMinutes = max(worstMinutes, d)
				if d > 3 {
					t.Errorf("%s: %s %s, the frontend's %s", name, p.what, p.got, p.want)
				}
			}
		}
		if r.PreviousFullMoon != c.PreviousFullMoon || r.NextFullMoon != c.NextFullMoon || r.PreviousNewMoon != c.PreviousNewMoon || r.NextNewMoon != c.NextNewMoon {
			t.Errorf("%s: full %s/%s new %s/%s, the frontend's %s/%s %s/%s", name, r.PreviousFullMoon, r.NextFullMoon, r.PreviousNewMoon, r.NextNewMoon,
				c.PreviousFullMoon, c.NextFullMoon, c.PreviousNewMoon, c.NextNewMoon)
		}
	}
	t.Logf("worst: %.3f° apart, moonrise/moonset %d minutes", worstAngle, worstMinutes)
}

// The phase instants themselves against astronomy-engine's (world/astro.ts SearchMoonPhase), to the minute.
func TestMoonPhasesMatchAstronomyEngine(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		target float64
		want   string
	}{{0, "2026-10-10T15:50"}, {180, "2026-10-26T04:12"}} {
		_, at := phaseAround(start, c.target)
		if got := at.UTC().Format("2006-01-02T15:04"); got != c.want {
			// Within ten minutes: Meeus's series against astronomy-engine.
			want, _ := time.Parse("2006-01-02T15:04", c.want)
			if d := at.Sub(want).Minutes(); math.Abs(d) > 10 {
				t.Errorf("phase %g: %s, astronomy-engine %s", c.target, got, c.want)
			}
		}
	}
}

func TestMoonReportTurnsWithNorth(t *testing.T) {
	fields := map[string]any{"date": "2026-10-26", "local_time": "22:00", "latitude_deg": 39.9042, "longitude_deg": 116.4074, "timezone": "Asia/Shanghai"}
	r := moonReport(physical(fields))
	fields["north_azimuth_deg"] = 90.0
	turned := moonReport(physical(fields))
	if r.CompassDeg != turned.CompassDeg || math.Abs(turned.SceneAzimuthDeg-round1(documentAzimuth(r.CompassDeg, 90))) > 0.11 {
		t.Fatalf("north 180: %+v; north 90: %+v", r, turned)
	}
	if r.Phase != "full" || r.Illumination < 0.99 || r.ElevationDeg < 40 {
		t.Fatalf("Beijing's full moon at 22:00: %+v", r)
	}
}

func TestMoonReportOnlyForALocatedPhysicalSky(t *testing.T) {
	elevation, azimuth, turbidity := 35.0, 135.0, 3.0
	if r := moonReport(Sky{Kind: "procedural", SunElevationDeg: &elevation, SunAzimuthDeg: &azimuth, Turbidity: &turbidity}); r != nil {
		t.Fatalf("procedural: %+v", r)
	}
	if r := moonReport(physical(map[string]any{"date": "2026-06-21", "sun_mode": "manual"})); r != nil {
		t.Fatalf("manual: %+v", r)
	}
	off := false
	if r := moonReport(physical(map[string]any{"date": "2026-06-21", "night": map[string]any{"moon": off}})); r == nil {
		t.Fatal("the moon switched off is still in the sky")
	}
}
